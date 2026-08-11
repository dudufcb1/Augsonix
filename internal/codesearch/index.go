package codesearch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Index mantiene el índice semántico de un workspace al día. Reindexa solo lo
// que cambió: embeber es lo caro, tanto en cuota como en tiempo.
type Index struct {
	root     string
	store    VectorStore
	state    *State
	embedder Embedder
	reranker Reranker
	progress progress
	// syncing serializa los escaneos: el arranque lanza uno en segundo plano y
	// el watcher puede pedir otro. Dos a la vez se pisarían el estado y
	// embeberían dos veces lo mismo, cobrándolo dos veces.
	syncing sync.Mutex
}

// Progress informa el avance de un escaneo, para que un frontend pueda
// mostrarlo sin que el indexador sepa nada de interfaces.
type Progress struct {
	File      string
	Done      int
	Total     int
	Embedded  int
	Unchanged int
}

// Stats resume el resultado de un escaneo.
type Stats struct {
	Scanned   int
	Embedded  int
	Unchanged int
	Removed   int
	Chunks    int
}

// NewIndex arma un índice sobre root. store y state deben venir ya abiertos con
// el mismo modelo que embedder, o los vectores guardados no serán comparables
// con los nuevos.
func NewIndex(root string, store VectorStore, state *State, embedder Embedder, reranker Reranker) *Index {
	return &Index{root: root, store: store, state: state, embedder: embedder, reranker: reranker}
}

// Sync recorre el workspace y deja el índice reflejando el disco: embebe los
// archivos nuevos o modificados, y saca los que desaparecieron. onProgress
// puede ser nil.
func (ix *Index) Sync(ctx context.Context, onProgress func(Progress)) (Stats, error) {
	ix.syncing.Lock()
	defer ix.syncing.Unlock()

	first := ix.state.Len() == 0
	ix.progress.set(func(s *Status) { *s = Status{Phase: PhaseScanning, First: first} })

	files, err := ix.collect(ctx)
	if err != nil {
		ix.fail(err)
		return Stats{}, err
	}
	ix.progress.set(func(s *Status) { s.Total = len(files) })

	var st Stats
	seen := make(map[string]bool, len(files))
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			ix.fail(err)
			return st, err
		}
		seen[f] = true
		st.Scanned++

		changed, err := ix.syncFile(ctx, f)
		switch {
		case errors.Is(err, ErrQuotaExhausted):
			// Sin cuota no hay nada que hacer con los archivos que faltan:
			// seguir solo acumularía el mismo error miles de veces.
			ix.fail(err)
			return st, err
		case err != nil:
			continue // un archivo ilegible no debe abortar el escaneo completo
		case changed:
			st.Embedded++
		default:
			st.Unchanged++
		}
		ix.progress.set(func(s *Status) {
			s.Done, s.Embedded = i+1, st.Embedded
			// Solo cuenta como indexado cuando de verdad se esta embebiendo;
			// un escaneo sin cambios no debe alarmar con una barra de progreso.
			if st.Embedded > 0 {
				s.Phase = PhaseIndexing
			}
		})
		if onProgress != nil {
			onProgress(Progress{File: f, Done: i + 1, Total: len(files), Embedded: st.Embedded, Unchanged: st.Unchanged})
		}
	}

	// Lo que quedó en el estado y ya no está en disco se fue del workspace.
	for _, p := range ix.state.Paths() {
		if !seen[p] {
			ix.store.Delete(p)
			ix.state.Delete(p)
			st.Removed++
		}
	}

	_, st.Chunks = ix.store.Stats()
	if err := ix.store.Save(); err != nil {
		ix.fail(err)
		return st, err
	}
	if err := ix.state.Save(); err != nil {
		ix.fail(err)
		return st, err
	}
	ix.progress.set(func(s *Status) {
		s.Phase, s.Chunks, s.Err = PhaseReady, st.Chunks, nil
	})
	return st, nil
}

// syncFile reindexa un archivo si su contenido cambió. Devuelve si hubo que
// embeberlo.
func (ix *Index) syncFile(ctx context.Context, rel string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(ix.root, filepath.FromSlash(rel)))
	if err != nil {
		return false, err
	}
	content := string(data)
	hash := FileHash(content)
	// El store también tiene que tenerlo: si el índice se borró pero el estado
	// sobrevivió, saltarse el archivo lo dejaría fuera para siempre.
	if ix.state.Unchanged(rel, hash) && ix.store.Has(rel) {
		return false, nil
	}

	chunks := ChunkFile(rel, content)
	if len(chunks) == 0 {
		ix.store.Delete(rel)
		ix.state.Set(rel, hash)
		return false, nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vecs, err := ix.embedder.Embed(ctx, texts, KindDocument)
	if err != nil {
		return false, err
	}

	flat := make([]int8, 0, len(vecs)*ix.store.Dims())
	for _, v := range vecs {
		flat = append(flat, v...)
	}
	if err := ix.store.Replace(rel, chunks, flat); err != nil {
		return false, err
	}
	ix.state.Set(rel, hash)
	return true, nil
}

// collect lista los archivos indexables del workspace, en rutas relativas con
// separador "/" para que el índice sea portable entre sistemas.
func (ix *Index) collect(ctx context.Context) ([]string, error) {
	m := newMatcher(ix.root)
	var out []string
	err := filepath.WalkDir(ix.root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil // un directorio sin permisos no aborta el recorrido
		}
		rel, err := filepath.Rel(ix.root, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if m.skipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if m.skipFile(rel, d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || !Indexable(rel, info.Size()) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// IndexDir es donde vive el índice de un workspace.
func IndexDir(root string) string {
	return filepath.Join(root, ".reasonix", "codesearch")
}

// Ready reporta cuántos chunks hay indexados y si ya se puede buscar. Un índice
// vacío no es un error: es uno que todavía no termina de construirse.
func (ix *Index) Ready() (int, bool) {
	_, chunks := ix.store.Stats()
	return chunks, chunks > 0
}

// fail deja el error a la vista en vez de que el índice se quede callado y la
// interfaz muestre un escaneo que nunca termina.
func (ix *Index) fail(err error) {
	phase := PhaseFailed
	if errors.Is(err, ErrQuotaExhausted) {
		phase = PhaseQuota
	}
	ix.progress.set(func(s *Status) { s.Phase, s.Err = phase, err })
}
