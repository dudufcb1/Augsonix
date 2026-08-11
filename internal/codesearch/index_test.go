package codesearch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbedder produce vectores deterministas a partir del texto, para probar
// el flujo sin llamar al proveedor ni gastar cuota.
type fakeEmbedder struct {
	dims  int
	calls int
	texts []string
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string, _ InputKind) ([][]int8, error) {
	f.calls++
	f.texts = append(f.texts, texts...)
	out := make([][]int8, len(texts))
	for i, t := range texts {
		v := make([]int8, f.dims)
		for j := range v {
			// Un vector estable por texto: mismo contenido, mismo vector.
			v[j] = int8((len(t)*7 + j*13 + int(t[0])) % 100)
		}
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) Dims() int     { return f.dims }
func (f *fakeEmbedder) Model() string { return "fake" }

// writeFile crea un archivo con contenido suficiente para producir un chunk.
func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTestIndex arma un índice sobre un workspace temporal con embebedor falso.
func newTestIndex(t *testing.T) (*Index, string, *fakeEmbedder) {
	t.Helper()
	root := t.TempDir()
	emb := &fakeEmbedder{dims: 8}
	store, err := OpenLocalStore(filepath.Join(root, ".reasonix", "codesearch"), "fake", 8)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(filepath.Join(root, ".reasonix", "codesearch"))
	if err != nil {
		t.Fatal(err)
	}
	return NewIndex(root, store, state, emb, nil), root, emb
}

// body genera contenido de código lo bastante largo para pasar el mínimo.
func body(marker string) string {
	return strings.Repeat("// "+marker+" line of source\n", 20)
}

func TestSyncIndexesSupportedFiles(t *testing.T) {
	// Un escaneo inicial embebe los archivos de código y deja el índice listo
	// para buscar.
	ix, root, emb := newTestIndex(t)
	writeFile(t, root, "internal/auth.go", body("auth"))
	writeFile(t, root, "internal/render.ts", body("render"))

	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Embedded != 2 {
		t.Errorf("se embebieron %d archivos, esperaba 2", st.Embedded)
	}
	if emb.calls == 0 {
		t.Error("no se llamó al embebedor")
	}
}

func TestSyncSkipsUnchangedFilesOnSecondRun(t *testing.T) {
	// Este es el punto del indexado incremental: la segunda corrida no debe
	// volver a embeber nada, porque embeber es lo que cuesta cuota y tiempo.
	ix, root, emb := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := emb.calls

	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("segundo Sync devolvió error: %v", err)
	}
	if st.Embedded != 0 {
		t.Errorf("se reembebieron %d archivos sin cambios", st.Embedded)
	}
	if st.Unchanged != 1 {
		t.Errorf("Unchanged = %d, esperaba 1", st.Unchanged)
	}
	if emb.calls != callsAfterFirst {
		t.Errorf("el embebedor se llamó %d veces extra", emb.calls-callsAfterFirst)
	}
}

func TestSyncReembedsModifiedFile(t *testing.T) {
	// Si el archivo cambió, su contenido viejo ya no debe estar en el índice.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("viejo"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "a.go", body("nuevo"))
	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Embedded != 1 {
		t.Errorf("Embedded = %d, esperaba 1 tras modificar el archivo", st.Embedded)
	}
}

func TestSyncRemovesDeletedFiles(t *testing.T) {
	// Un archivo borrado del disco tiene que salir del índice, o la búsqueda
	// devolvería rutas que ya no existen.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	writeFile(t, root, "b.go", body("beta"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
		t.Fatal(err)
	}
	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Removed != 1 {
		t.Errorf("Removed = %d, esperaba 1", st.Removed)
	}
	if ix.store.Has("b.go") {
		t.Error("el archivo borrado sigue en el índice")
	}
}

func TestSyncIgnoresVendorAndUnsupportedFiles(t *testing.T) {
	// node_modules y los binarios no aportan a la búsqueda y sí gastarían
	// cuota; no deben llegar al embebedor.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "app.go", body("app"))
	writeFile(t, root, "node_modules/dep/index.js", body("dep"))
	writeFile(t, root, "logo.png", body("binario"))

	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Scanned != 1 {
		t.Errorf("se escanearon %d archivos, esperaba solo app.go", st.Scanned)
	}
}

func TestSyncHonorsGitignore(t *testing.T) {
	// Lo que el proyecto declaró ignorado no entra al índice.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, ".gitignore", "generated/\n")
	writeFile(t, root, "app.go", body("app"))
	writeFile(t, root, "generated/api.go", body("generado"))

	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Scanned != 1 {
		t.Errorf("se escanearon %d archivos, el .gitignore no se respetó", st.Scanned)
	}
}

func TestSyncReportsProgress(t *testing.T) {
	// El avance permite que un frontend muestre el escaneo sin que el
	// indexador sepa nada de interfaces.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	writeFile(t, root, "b.go", body("beta"))

	var seen []Progress
	if _, err := ix.Sync(context.Background(), func(p Progress) { seen = append(seen, p) }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("se reportaron %d avances, esperaba 2", len(seen))
	}
	if seen[len(seen)-1].Done != seen[len(seen)-1].Total {
		t.Error("el último avance no llegó al total")
	}
}

func TestSyncRebuildsWhenIndexLostButStateSurvived(t *testing.T) {
	// Si alguien borra el índice pero queda el estado, saltarse los archivos
	// "sin cambios" los dejaría fuera para siempre.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	empty, err := OpenLocalStore(filepath.Join(t.TempDir(), "vacio"), "fake", 8)
	if err != nil {
		t.Fatal(err)
	}
	ix.store = empty

	st, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync devolvió error: %v", err)
	}
	if st.Embedded != 1 {
		t.Errorf("Embedded = %d, esperaba reconstruir el archivo faltante", st.Embedded)
	}
}
