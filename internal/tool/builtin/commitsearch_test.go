package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/codesearch"
)

// fakeCommitIndex responde lo que le digan, para probar la tool sin repositorio
// ni proveedor de embeddings.
type fakeCommitIndex struct {
	ready   int
	results []codesearch.CommitResult
	query   string
	limit   int
}

func (f *fakeCommitIndex) Ready() (int, bool) { return f.ready, f.ready > 0 }

func (f *fakeCommitIndex) Search(_ context.Context, query string, limit int) ([]codesearch.CommitResult, error) {
	f.query, f.limit = query, limit
	return f.results, nil
}

func TestNewCommitSearchIsNilWithoutIndex(t *testing.T) {
	// Sin índice la tool no se registra: una que no puede responder seguiría
	// costando tokens de prefijo en cada turno de cada sesión.
	if got := NewCommitSearch(nil); got != nil {
		t.Error("devolvió una tool sin índice detrás")
	}
}

func TestCommitSearchReportsAnEmptyIndexWithoutFailing(t *testing.T) {
	// Un índice vacío no es un error: puede estar construyéndose o la carpeta
	// puede no ser un repositorio. El modelo necesita saberlo para no concluir
	// que el cambio nunca ocurrió.
	tl := NewCommitSearch(&fakeCommitIndex{ready: 0})
	got, err := tl.Execute(context.Background(), json.RawMessage(`{"request":"algo"}`))
	if err != nil {
		t.Fatalf("devolvió error en vez de explicarse: %v", err)
	}
	if !strings.Contains(got, "empty") || !strings.Contains(got, "git log") {
		t.Errorf("la respuesta no orienta al modelo: %q", got)
	}
}

func TestCommitSearchRequiresARequest(t *testing.T) {
	// Una consulta vacía gastaría cuota para nada.
	tl := NewCommitSearch(&fakeCommitIndex{ready: 5})
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"request":"  "}`)); err == nil {
		t.Error("aceptó una petición vacía")
	}
}

func TestCommitSearchCapsTheLimit(t *testing.T) {
	// Un límite disparatado llenaría el contexto de commits.
	idx := &fakeCommitIndex{ready: 5}
	tl := NewCommitSearch(idx)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"request":"x","limit":500}`)); err != nil {
		t.Fatal(err)
	}
	if idx.limit != commitSearchMaxLimit {
		t.Errorf("límite = %d, esperaba el tope de %d", idx.limit, commitSearchMaxLimit)
	}
}

func TestCommitSearchShowsMessagesWithoutDiffs(t *testing.T) {
	// El resultado trae el hash y el mensaje, que es lo que responde la
	// pregunta. El diff se queda fuera: diez resultados arrastrarían diez
	// diffs al contexto, y con el hash se puede pedir a git el que interese.
	idx := &fakeCommitIndex{ready: 2, results: []codesearch.CommitResult{{
		Hash:    "abc1234def",
		Score:   0.91,
		Content: "arregla el cobro duplicado\n\nla pasarela reintentaba\n\nArchivos: Billing.php\n\ndiff --git a/Billing.php b/Billing.php\n-viejo\n+nuevo\n",
	}}}
	tl := NewCommitSearch(idx)
	got, err := tl.Execute(context.Background(), json.RawMessage(`{"request":"cobros repetidos"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"abc1234def", "arregla el cobro duplicado", "la pasarela reintentaba", "Billing.php"} {
		if !strings.Contains(got, want) {
			t.Errorf("el resultado no incluye %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "+nuevo") || strings.Contains(got, "diff --git") {
		t.Errorf("el resultado arrastró el diff:\n%s", got)
	}
	if idx.query != "cobros repetidos" {
		t.Errorf("la consulta llegó como %q", idx.query)
	}
}

func TestCommitSearchExplainsWhenNothingMatches(t *testing.T) {
	// Cero resultados con una salida que le diga al modelo qué hacer después.
	tl := NewCommitSearch(&fakeCommitIndex{ready: 3})
	got, err := tl.Execute(context.Background(), json.RawMessage(`{"request":"algo que no existe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "No matching commits") {
		t.Errorf("salida inesperada: %q", got)
	}
}
