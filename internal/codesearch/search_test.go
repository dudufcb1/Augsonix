package codesearch

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// fakeReranker invierte el orden recibido, que basta para distinguir si el
// resultado vino del reranker o del barrido vectorial.
type fakeReranker struct {
	err   error
	calls int
	lastN int
	lastQ string
}

func (f *fakeReranker) Rerank(_ context.Context, query string, docs []string, topK int) ([]Ranked, error) {
	f.calls++
	f.lastN = len(docs)
	f.lastQ = query
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Ranked, 0, len(docs))
	for i := range slices.Backward(docs) {
		out = append(out, Ranked{Index: i, Score: float32(len(docs) - i)})
	}
	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

// indexWithChunks arma un índice ya poblado, sin tocar disco de trabajo.
func indexWithChunks(t *testing.T, rr Reranker, paths ...string) *Index {
	t.Helper()
	dir := t.TempDir()
	emb := &fakeEmbedder{dims: 8}
	store, err := OpenLocalStore(dir, "fake", 8)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := LoadState(dir)
	for _, p := range paths {
		vecs, err := emb.Embed(context.Background(), []string{p}, KindDocument)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Replace(p, "h", []Chunk{chunkAt(p, p)}, vecs[0]); err != nil {
			t.Fatal(err)
		}
	}
	return NewIndex(dir, store, state, emb, rr)
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	// Sin consulta no hay nada que embeber; fallar temprano evita gastar una
	// llamada al proveedor para no obtener nada.
	ix := indexWithChunks(t, nil, "a.go")
	if _, err := ix.Search(context.Background(), "   ", SearchOptions{}); err == nil {
		t.Error("esperaba error con una consulta vacía")
	}
}

func TestSearchUsesRerankerOrder(t *testing.T) {
	// Cuando hay reranker, el orden final lo decide él: es la única etapa que
	// lee consulta y chunk juntos.
	rr := &fakeReranker{}
	ix := indexWithChunks(t, rr, "a.go", "b.go", "c.go")

	got, err := ix.Search(context.Background(), "buscar algo", SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Search devolvió error: %v", err)
	}
	if rr.calls != 1 {
		t.Fatalf("el reranker se llamó %d veces, esperaba 1", rr.calls)
	}
	if len(got) == 0 || !got[0].Reranked {
		t.Error("el resultado no quedó marcado como reordenado")
	}
}

func TestSearchFallsBackWhenRerankerFails(t *testing.T) {
	// Un reranker caído degrada el orden, no la búsqueda: el barrido vectorial
	// ya trajo candidatos utilizables y devolverlos es mejor que un error.
	rr := &fakeReranker{err: errors.New("502 bad gateway")}
	ix := indexWithChunks(t, rr, "a.go", "b.go")

	got, err := ix.Search(context.Background(), "buscar algo", SearchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("Search devolvió error pese al respaldo: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no devolvió resultados tras fallar el reranker")
	}
	if got[0].Reranked {
		t.Error("el resultado se marcó como reordenado sin que el reranker respondiera")
	}
}

func TestSearchSendsMoreCandidatesThanResults(t *testing.T) {
	// El barrido vectorial es barato y el reranker cuesta por documento: se le
	// dan más candidatos de los que se van a devolver, que es lo que sube el
	// acierto sin disparar el gasto.
	rr := &fakeReranker{}
	paths := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"}
	ix := indexWithChunks(t, rr, paths...)

	got, err := ix.Search(context.Background(), "buscar", SearchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("Search devolvió error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("devolvió %d resultados, el límite era 2", len(got))
	}
	if rr.lastN <= 2 {
		t.Errorf("al reranker le llegaron %d candidatos, esperaba más que el límite", rr.lastN)
	}
}

func TestSearchWithoutRerankerReturnsVectorOrder(t *testing.T) {
	// Sin reranker configurado la búsqueda sigue sirviendo, solo con el orden
	// del coseno.
	ix := indexWithChunks(t, nil, "a.go", "b.go")

	got, err := ix.Search(context.Background(), "buscar", SearchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("Search devolvió error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no devolvió resultados")
	}
	if got[0].Reranked {
		t.Error("se marcó como reordenado sin reranker configurado")
	}
}

func TestSearchFiltersByPathPrefix(t *testing.T) {
	// Acotar a un subárbol deja preguntar por un módulo concreto sin que se
	// cuelen resultados del resto del repositorio.
	ix := indexWithChunks(t, nil, "internal/auth/token.go", "desktop/app.go")

	got, err := ix.Search(context.Background(), "buscar", SearchOptions{Limit: 5, PathPrefix: "internal/"})
	if err != nil {
		t.Fatalf("Search devolvió error: %v", err)
	}
	for _, r := range got {
		if r.Chunk.Path != "internal/auth/token.go" {
			t.Errorf("se coló %q fuera del prefijo pedido", r.Chunk.Path)
		}
	}
}

func TestSearchOptionsDefaults(t *testing.T) {
	// El cero de SearchOptions tiene que dar una búsqueda razonable, para que
	// quien la llame no tenga que conocer los topes internos.
	got := SearchOptions{}.normalized()
	if got.Limit != defaultSearchLimit {
		t.Errorf("Limit = %d, esperaba %d", got.Limit, defaultSearchLimit)
	}
	if got.Candidates < got.Limit {
		t.Errorf("Candidates = %d, no puede ser menor que Limit = %d", got.Candidates, got.Limit)
	}
	if capped := (SearchOptions{Limit: 100, Candidates: 9999}).normalized(); capped.Candidates > maxCandidates {
		t.Errorf("Candidates = %d, excede el tope de %d", capped.Candidates, maxCandidates)
	}
}
