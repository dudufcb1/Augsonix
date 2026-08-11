package codesearch

import (
	"context"
	"fmt"
	"strings"
)

const (
	// DefaultCommitLimit es cuántos commits se indexan hacia atrás. La historia
	// vieja se busca poco y cada commit cuesta cuota, así que se acota.
	DefaultCommitLimit = 1000
	// commitEmbedBatch agrupa commits por llamada. Cada documento ronda los
	// 1200 tokens, así que un lote de 32 queda holgado bajo el techo por
	// petición del proveedor.
	commitEmbedBatch = 32
)

// CommitIndex mantiene indexada la historia de un repositorio. Responde a "cómo
// se hizo algo parecido antes", que es lo que el índice de código no puede: el
// código dice cómo está ahora, no por qué se llegó ahí.
type CommitIndex struct {
	root     string
	store    VectorStore
	embedder Embedder
	reranker Reranker
	limit    int
	recipe   string
}

// NewCommitIndex arma el índice sobre un almacén propio. store debe estar
// separado del de código: son dos búsquedas distintas y mezclarlas haría que
// una consulta sobre código devolviera commits.
func NewCommitIndex(root string, store VectorStore, embedder Embedder, reranker Reranker, limit int) *CommitIndex {
	if limit <= 0 {
		limit = DefaultCommitLimit
	}
	return &CommitIndex{
		root: root, store: store, embedder: embedder, reranker: reranker, limit: limit,
		recipe: IndexRecipe(embedder.Model(), embedder.Dims()),
	}
}

// CommitStats resume un escaneo de la historia.
type CommitStats struct {
	Scanned   int
	Embedded  int
	Unchanged int
	Removed   int
}

// Sync deja el índice reflejando la historia. Un commit es inmutable, así que
// lo ya indexado no se vuelve a tocar salvo que cambie la receta; lo que sí
// puede desaparecer es un commit reescrito por un rebase.
func (ix *CommitIndex) Sync(ctx context.Context, onProgress func(CommitStats)) (CommitStats, error) {
	var stats CommitStats
	commits, err := ExtractCommits(ctx, ix.root, ix.limit)
	if err != nil {
		return stats, err
	}
	stats.Scanned = len(commits)

	pending := make([]Commit, 0, len(commits))
	seen := make(map[string]bool, len(commits))
	for _, c := range commits {
		seen[c.Hash] = true
		if indexed, ok := ix.store.FileHash(c.Hash); ok && indexed == ix.hashOf(c) {
			stats.Unchanged++
			continue
		}
		pending = append(pending, c)
	}

	for start := 0; start < len(pending); start += commitEmbedBatch {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		end := min(start+commitEmbedBatch, len(pending))
		if err := ix.embedBatch(ctx, pending[start:end]); err != nil {
			return stats, err
		}
		stats.Embedded += end - start
		if onProgress != nil {
			onProgress(stats)
		}
	}

	for _, path := range ix.store.Paths() {
		if !seen[path] {
			ix.store.Delete(path)
			stats.Removed++
		}
	}
	if err := ix.store.Save(); err != nil {
		return stats, err
	}
	return stats, nil
}

func (ix *CommitIndex) hashOf(c Commit) string {
	return FileHash(ix.recipe, c.Document())
}

// embedBatch embebe un grupo de commits y los guarda. Cada commit es un solo
// bloque: partirlo separaría el mensaje del cambio que describe.
func (ix *CommitIndex) embedBatch(ctx context.Context, commits []Commit) error {
	texts := make([]string, len(commits))
	for i, c := range commits {
		texts[i] = c.Document()
	}
	vecs, err := ix.embedder.Embed(ctx, texts, KindDocument)
	if err != nil {
		return err
	}
	if len(vecs) != len(commits) {
		return fmt.Errorf("codesearch: se esperaban %d vectores, llegaron %d", len(commits), len(vecs))
	}
	for i, c := range commits {
		chunk := Chunk{
			Path:    c.Hash,
			Content: texts[i],
			Hash:    ix.hashOf(c),
		}
		if err := ix.store.Replace(c.Hash, chunk.Hash, []Chunk{chunk}, vecs[i]); err != nil {
			return err
		}
	}
	return nil
}

// CommitResult es un commit recuperado por una búsqueda.
type CommitResult struct {
	Hash    string
	Score   float32
	Content string
}

// Subject es la primera línea del documento, que es el asunto del commit.
func (r CommitResult) Subject() string {
	line, _, _ := strings.Cut(r.Content, "\n")
	return line
}

// Message devuelve el mensaje y los archivos, sin el diff. Es lo que responde
// la pregunta; quien quiera ver el cambio tiene el hash para pedirlo a git, y
// así diez resultados no arrastran diez diffs al contexto.
func (r CommitResult) Message() string {
	if idx := strings.Index(r.Content, "\ndiff --git "); idx > 0 {
		return strings.TrimSpace(r.Content[:idx])
	}
	return strings.TrimSpace(r.Content)
}

// Ready reporta cuántos commits hay indexados y si se puede buscar.
func (ix *CommitIndex) Ready() (int, bool) {
	_, chunks := ix.store.Stats()
	return chunks, chunks > 0
}

// Search responde una consulta sobre la historia. Misma estrategia que el
// índice de código: barrido vectorial amplio y reranker sobre los candidatos.
func (ix *CommitIndex) Search(ctx context.Context, query string, limit int) ([]CommitResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("codesearch: la consulta está vacía")
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	vecs, err := ix.embedder.Embed(ctx, []string{query}, KindQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("codesearch: se esperaba 1 vector de consulta, llegaron %d", len(vecs))
	}
	matches := ix.store.Search(vecs[0], limit*defaultCandidateFactor)
	if len(matches) == 0 {
		return nil, nil
	}
	if ix.reranker == nil {
		return commitResults(matches, limit), nil
	}

	docs := make([]string, len(matches))
	for i, m := range matches {
		docs[i] = m.Chunk.Content
	}
	ranked, err := ix.reranker.Rerank(ctx, query, docs, limit)
	if err != nil {
		return commitResults(matches, limit), nil
	}
	out := make([]CommitResult, 0, len(ranked))
	for _, r := range ranked {
		m := matches[r.Index]
		out = append(out, CommitResult{Hash: m.Chunk.Path, Score: r.Score, Content: m.Chunk.Content})
	}
	return out, nil
}

func commitResults(matches []Match, limit int) []CommitResult {
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]CommitResult, len(matches))
	for i, m := range matches {
		out[i] = CommitResult{Hash: m.Chunk.Path, Score: m.Score, Content: m.Chunk.Content}
	}
	return out
}
