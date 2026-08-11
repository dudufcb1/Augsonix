package codesearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// voyageServer levanta un servidor falso de Voyage que responde con handler.
func voyageServer(t *testing.T, handler http.HandlerFunc) *Voyage {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Voyage{
		APIKey:      "test-key",
		EmbedModel:  "voyage-code-3",
		RerankModel: "rerank-2.5",
		Dimensions:  3,
		BaseURL:     srv.URL,
		HTTP:        srv.Client(),
	}
}

func TestVoyageEmbedRestoresProviderOrder(t *testing.T) {
	// La API no promete devolver los vectores en el orden de entrada, solo el
	// campo index. Si se confiara en el orden de llegada, cada chunk quedaría
	// con el vector de otro y la búsqueda devolvería archivos al azar.
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 2, "embedding": []int{3, 3, 3}},
				{"index": 0, "embedding": []int{1, 1, 1}},
				{"index": 1, "embedding": []int{2, 2, 2}},
			},
		})
	})

	got, err := v.Embed(context.Background(), []string{"a", "b", "c"}, KindDocument)
	if err != nil {
		t.Fatalf("Embed devolvió error: %v", err)
	}
	for i, want := range []int8{1, 2, 3} {
		if got[i][0] != want {
			t.Errorf("vector %d empieza con %d, esperaba %d", i, got[i][0], want)
		}
	}
}

func TestVoyageEmbedSendsInputTypeAndDtype(t *testing.T) {
	// input_type distingue consulta de documento (el proveedor antepone
	// instrucciones distintas) y output_dtype pide int8, que es lo que guarda
	// el índice.
	var got voyageEmbedRequest
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []int{1, 1, 1}}},
		})
	})

	if _, err := v.Embed(context.Background(), []string{"buscar auth"}, KindQuery); err != nil {
		t.Fatalf("Embed devolvió error: %v", err)
	}
	if got.InputType != "query" {
		t.Errorf("input_type = %q, esperaba query", got.InputType)
	}
	if got.OutputDtype != "int8" {
		t.Errorf("output_dtype = %q, esperaba int8", got.OutputDtype)
	}
	if got.OutputDimension != 3 {
		t.Errorf("output_dimension = %d, esperaba 3", got.OutputDimension)
	}
}

func TestVoyageEmbedRejectsWrongDimension(t *testing.T) {
	// Un vector de otra dimensión desalinearía el store entero, así que se
	// rechaza en vez de guardarse.
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []int{1, 1, 1, 1, 1}}},
		})
	})

	if _, err := v.Embed(context.Background(), []string{"a"}, KindDocument); err == nil {
		t.Error("esperaba error por dimensión inesperada")
	}
}

func TestVoyageEmbedRetriesRateLimit(t *testing.T) {
	// Durante un escaneo inicial se disparan cientos de lotes seguidos y el 429
	// es esperable; abandonar ahí dejaría el índice a medias.
	calls := 0
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"detail":"rate limit"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []int{1, 2, 3}}},
		})
	})

	got, err := v.Embed(context.Background(), []string{"a"}, KindDocument)
	if err != nil {
		t.Fatalf("Embed devolvió error tras el reintento: %v", err)
	}
	if calls != 2 {
		t.Errorf("hubo %d llamadas, esperaba 2 (una fallida y el reintento)", calls)
	}
	if len(got) != 1 {
		t.Errorf("se recuperaron %d vectores, esperaba 1", len(got))
	}
}

func TestVoyageEmbedDoesNotRetryBadRequest(t *testing.T) {
	// Un 400 es un error de la petición: reintentarlo solo quema cuota y
	// retrasa el mensaje real, que aquí es la credencial o el modelo.
	calls := 0
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"modelo desconocido"}`))
	})

	_, err := v.Embed(context.Background(), []string{"a"}, KindDocument)
	if err == nil {
		t.Fatal("esperaba error")
	}
	if calls != 1 {
		t.Errorf("hubo %d llamadas, un 400 no debe reintentarse", calls)
	}
	if !strings.Contains(err.Error(), "modelo desconocido") {
		t.Errorf("el error perdió el detalle de la API: %v", err)
	}
}

func TestVoyageRerankReturnsIndexesIntoInput(t *testing.T) {
	// El reranker responde con posiciones del slice enviado; si se
	// malinterpretaran, se mostraría el chunk equivocado con el score de otro.
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 2, "relevance_score": 0.91},
				{"index": 0, "relevance_score": 0.42},
			},
		})
	})

	got, err := v.Rerank(context.Background(), "cómo se valida el token", []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatalf("Rerank devolvió error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("devolvió %d resultados, esperaba 2", len(got))
	}
	if got[0].Index != 2 || got[0].Score < 0.9 {
		t.Errorf("primer resultado = %+v, esperaba índice 2 con score alto", got[0])
	}
}

func TestVoyageRerankDropsOutOfRangeIndexes(t *testing.T) {
	// Un índice fuera de rango indexaría fuera del slice al formatear el
	// resultado; se descarta en vez de reventar la búsqueda entera.
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 99, "relevance_score": 0.99},
				{"index": 1, "relevance_score": 0.5},
			},
		})
	})

	got, err := v.Rerank(context.Background(), "q", []string{"a", "b"}, 2)
	if err != nil {
		t.Fatalf("Rerank devolvió error: %v", err)
	}
	if len(got) != 1 || got[0].Index != 1 {
		t.Errorf("resultados = %+v, esperaba solo el índice válido", got)
	}
}

func TestVoyageRerankSkipsCallWithoutDocuments(t *testing.T) {
	// Sin candidatos no hay nada que reordenar: llamar igual gastaría una
	// petición cobrada para recibir una lista vacía.
	called := false
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })

	got, err := v.Rerank(context.Background(), "q", nil, 5)
	if err != nil || got != nil {
		t.Errorf("esperaba resultado vacío sin error, hubo (%v, %v)", got, err)
	}
	if called {
		t.Error("se llamó a la API sin documentos que reordenar")
	}
}

func TestBatchTextsSplitsByCount(t *testing.T) {
	// El proveedor no acepta más de 1000 textos por petición.
	texts := make([]string, voyageMaxBatchTexts+5)
	for i := range texts {
		texts[i] = "x"
	}
	got := batchTexts(texts)
	if len(got) != 2 {
		t.Fatalf("se armaron %d lotes, esperaba 2", len(got))
	}
	if len(got[0]) != voyageMaxBatchTexts {
		t.Errorf("el primer lote trae %d textos, esperaba %d", len(got[0]), voyageMaxBatchTexts)
	}
}

func TestBatchTextsSplitsByTokenBudget(t *testing.T) {
	// Pocos textos pueden rebasar el tope de tokens del lote; hay que partirlos
	// igual o la API rechaza la petición completa.
	big := strings.Repeat("a", voyageMaxBatchTokens*2)
	got := batchTexts([]string{big, big, big})
	if len(got) < 3 {
		t.Errorf("se armaron %d lotes para textos enormes, esperaba uno por texto", len(got))
	}
}
