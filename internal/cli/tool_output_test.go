package cli

import (
	"strings"
	"testing"
)

func TestToolOutputBlockShowsWhatTheToolReturned(t *testing.T) {
	// El punto de la vista: poder juzgar si a la herramienta le sirvió lo que
	// encontró. Con el resultado invisible no hay forma de saberlo.
	got := toolOutputBlock("grep", "internal/auth.go:12:func Authenticate\ninternal/auth.go:30:token", 80)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "Authenticate") {
		t.Errorf("no se mostró el contenido del resultado: %q", joined)
	}
}

func TestToolOutputBlockTrimsLongResults(t *testing.T) {
	// Un read_file de mil líneas taparía la conversación entera. Se corta y se
	// dice cuánto quedó fuera; el modelo ya recibió el resultado completo.
	got := toolOutputBlock("read_file", strings.Repeat("una linea de codigo\n", 200), 80)
	if len(got) > toolOutputPreviewLines+1 {
		t.Errorf("mostró %d líneas, el tope es %d más el aviso", len(got), toolOutputPreviewLines)
	}
	if !strings.Contains(got[len(got)-1], "more lines") {
		t.Errorf("no avisó cuántas líneas quedaron fuera: %q", got[len(got)-1])
	}
}

func TestToolOutputBlockStaysQuietForToolsWithTheirOwnView(t *testing.T) {
	// bash ya tiene su bloque en vivo con Ctrl+B y las de escritura se ven como
	// diff. Repetirlas aquí duplicaría lo mismo en pantalla.
	for _, name := range []string{"bash", "write_file", "edit_file", "multi_edit", "todo_write"} {
		if got := toolOutputBlock(name, "algo de salida", 80); got != nil {
			t.Errorf("%s se pintó dos veces: %v", name, got)
		}
	}
}

func TestToolOutputBlockIgnoresEmptyResults(t *testing.T) {
	// Una herramienta que no devolvió nada no merece una línea en blanco en el
	// historial.
	for _, out := range []string{"", "   ", "\n\n"} {
		if got := toolOutputBlock("grep", out, 80); got != nil {
			t.Errorf("un resultado vacío produjo %v", got)
		}
	}
}

func TestToolOutputBlockShowsSemanticSearchResults(t *testing.T) {
	// El caso que motivó esto: sin ver qué devolvió code_search no hay forma de
	// juzgar si el índice sirve.
	out := "2 results, most relevant first.\n\ninternal/auth/token.go:12-30 (score 0.81)\nfunc Authenticate() error {}"
	got := toolOutputBlock("code_search", out, 80)
	if got == nil {
		t.Fatal("no se mostró el resultado de code_search")
	}
	if !strings.Contains(strings.Join(got, "\n"), "internal/auth/token.go:12-30") {
		t.Error("se perdió la ubicación en la vista")
	}
}
