package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/codesearch"
)

// fakeIndex simula el índice semántico sin red ni disco.
type fakeIndex struct {
	results []codesearch.Result
	chunks  int
	err     error
	lastOpt codesearch.SearchOptions
	lastQ   string
}

func (f *fakeIndex) Search(_ context.Context, query string, opts codesearch.SearchOptions) ([]codesearch.Result, error) {
	f.lastQ, f.lastOpt = query, opts
	return f.results, f.err
}

func (f *fakeIndex) Ready() (int, bool) { return f.chunks, f.chunks > 0 }

func result(path string, start, end int, score float32, body string) codesearch.Result {
	return codesearch.Result{
		Chunk: codesearch.Chunk{Path: path, StartLine: start, EndLine: end, Content: body},
		Score: score,
	}
}

func TestNewCodeSearchReturnsNilWithoutIndex(t *testing.T) {
	// Sin índice la tool no debe registrarse: una tool inútil sigue pagando
	// tokens de prefijo en cada turno de cada sesión.
	if NewCodeSearch(nil) != nil {
		t.Error("se construyó la tool sin índice")
	}
}

func TestCodeSearchFormatsPathAndLineRange(t *testing.T) {
	// Cada resultado abre con path:inicio-fin para que el modelo pueda leer o
	// editar directo, sin gastar otra llamada en localizar el archivo.
	ix := &fakeIndex{chunks: 10, results: []codesearch.Result{
		result("internal/auth/token.go", 12, 30, 0.81, "func Authenticate() error {}"),
	}}
	got, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"validacion de sesion"}`))
	if err != nil {
		t.Fatalf("Execute devolvió error: %v", err)
	}
	if !strings.Contains(got, "internal/auth/token.go:12-30") {
		t.Errorf("falta la ubicación en la salida: %q", got)
	}
	if !strings.Contains(got, "func Authenticate() error {}") {
		t.Errorf("falta el contenido del chunk: %q", got)
	}
}

func TestCodeSearchRequiresRequest(t *testing.T) {
	// Sin consulta no hay nada que embeber; fallar temprano evita gastar una
	// llamada al proveedor para no obtener nada.
	ix := &fakeIndex{chunks: 5}
	if _, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"  "}`)); err == nil {
		t.Error("esperaba error con request vacío")
	}
}

func TestCodeSearchReportsEmptyIndexWithoutFailing(t *testing.T) {
	// Un índice vacío no es un error, es uno que aún se construye. Devolver
	// error haría que el modelo concluyera que el código no existe; el aviso lo
	// manda a grep en su lugar.
	ix := &fakeIndex{chunks: 0}
	got, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"lo que sea"}`))
	if err != nil {
		t.Fatalf("un índice vacío no debe ser error: %v", err)
	}
	if !strings.Contains(got, "grep") {
		t.Errorf("el aviso debería redirigir a grep, got %q", got)
	}
}

func TestCodeSearchCapsLimit(t *testing.T) {
	// El límite acota cuántos candidatos pasan al reranker, que es la etapa que
	// se cobra por documento.
	ix := &fakeIndex{chunks: 10}
	if _, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"x","limit":9999}`)); err != nil {
		t.Fatal(err)
	}
	if ix.lastOpt.Limit != codeSearchMaxLimit {
		t.Errorf("Limit = %d, esperaba el tope de %d", ix.lastOpt.Limit, codeSearchMaxLimit)
	}
}

func TestCodeSearchPassesPathPrefix(t *testing.T) {
	// Acotar a un subárbol permite preguntar por un módulo concreto sin que se
	// cuelen resultados del resto del repositorio.
	ix := &fakeIndex{chunks: 10}
	if _, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"x","path":"internal/agent/"}`)); err != nil {
		t.Fatal(err)
	}
	if ix.lastOpt.PathPrefix != "internal/agent/" {
		t.Errorf("PathPrefix = %q, no llegó al índice", ix.lastOpt.PathPrefix)
	}
}

func TestCodeSearchNoMatchesSuggestsGrep(t *testing.T) {
	// Cero resultados con el índice lleno significa que la consulta no pegó;
	// conviene decirle al modelo qué hacer en vez de dejarlo sin salida.
	ix := &fakeIndex{chunks: 10}
	got, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"algo que no existe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "No matches") || !strings.Contains(got, "grep") {
		t.Errorf("salida sin guía tras cero resultados: %q", got)
	}
}

func TestCodeSearchSurfacesIndexError(t *testing.T) {
	// Un fallo del proveedor sí debe llegar como error, para que no se
	// confunda con "no hay resultados".
	ix := &fakeIndex{chunks: 10, err: errors.New("401 unauthorized")}
	if _, err := NewCodeSearch(ix).Execute(context.Background(), json.RawMessage(`{"request":"x"}`)); err == nil {
		t.Error("esperaba que el error del índice llegara al llamador")
	}
}

func TestCodeSearchIsReadOnly(t *testing.T) {
	// Marcarla de solo lectura deja que el agente la paralelice con otras
	// lecturas en el mismo lote.
	if !NewCodeSearch(&fakeIndex{chunks: 1}).ReadOnly() {
		t.Error("code_search debería ser de solo lectura")
	}
}
