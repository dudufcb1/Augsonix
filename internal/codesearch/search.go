package codesearch

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultSearchLimit = 10
	// El barrido vectorial entrega candidatos baratos; el reranker es el que
	// cuesta por documento. Pedir de más al primero y recortar con el segundo
	// es lo que sube el acierto sin disparar el gasto.
	defaultCandidateFactor = 6
	maxCandidates          = 200
)

// SearchOptions ajusta una búsqueda. El cero vale: da un límite razonable y
// candidatos proporcionales.
type SearchOptions struct {
	// Limit son los resultados finales.
	Limit int
	// Candidates son los chunks que el barrido vectorial pasa al reranker.
	Candidates int
	// PathPrefix acota la búsqueda a un subárbol del workspace.
	PathPrefix string
}

// Result es un chunk recuperado, con la relevancia que le dio la última etapa
// que lo tocó: el reranker si corrió, el coseno del barrido si no.
type Result struct {
	Chunk    Chunk
	Score    float32
	Reranked bool
}

// Search responde una consulta en lenguaje natural. Va en dos etapas porque
// hacen cosas distintas: el barrido vectorial compara embeddings calculados por
// separado, sin saber qué se preguntó, y sirve para descartar rápido; el
// reranker lee consulta y chunk juntos y por eso ordena mucho mejor, pero solo
// puede aplicarse a un puñado de candidatos.
func (ix *Index) Search(ctx context.Context, query string, opts SearchOptions) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("codesearch: la consulta está vacía")
	}
	opts = opts.normalized()

	vecs, err := ix.embedder.Embed(ctx, []string{query}, KindQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("codesearch: se esperaba 1 vector de consulta, llegaron %d", len(vecs))
	}

	matches := ix.store.Search(vecs[0], opts.Candidates)
	matches = filterByPrefix(matches, opts.PathPrefix)
	if len(matches) == 0 {
		return nil, nil
	}
	if ix.reranker == nil {
		return trim(asResults(matches, false), opts.Limit), nil
	}

	docs := make([]string, len(matches))
	for i, m := range matches {
		docs[i] = m.Chunk.Content
	}
	ranked, err := ix.reranker.Rerank(ctx, query, docs, opts.Limit)
	if err != nil {
		// Un reranker caído degrada el orden, no la búsqueda: el barrido
		// vectorial ya trajo candidatos utilizables.
		return trim(asResults(matches, false), opts.Limit), nil
	}

	out := make([]Result, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, Result{Chunk: matches[r.Index].Chunk, Score: r.Score, Reranked: true})
	}
	return trim(out, opts.Limit), nil
}

func (o SearchOptions) normalized() SearchOptions {
	if o.Limit <= 0 {
		o.Limit = defaultSearchLimit
	}
	if o.Candidates <= 0 {
		o.Candidates = o.Limit * defaultCandidateFactor
	}
	if o.Candidates > maxCandidates {
		o.Candidates = maxCandidates
	}
	if o.Candidates < o.Limit {
		o.Candidates = o.Limit
	}
	o.PathPrefix = strings.TrimPrefix(o.PathPrefix, "./")
	return o
}

func filterByPrefix(matches []Match, prefix string) []Match {
	if prefix == "" {
		return matches
	}
	out := matches[:0]
	for _, m := range matches {
		if strings.HasPrefix(m.Chunk.Path, prefix) {
			out = append(out, m)
		}
	}
	return out
}

func asResults(matches []Match, reranked bool) []Result {
	out := make([]Result, len(matches))
	for i, m := range matches {
		out[i] = Result{Chunk: m.Chunk, Score: m.Score, Reranked: reranked}
	}
	return out
}

func trim(rs []Result, limit int) []Result {
	if len(rs) > limit {
		return rs[:limit]
	}
	return rs
}
