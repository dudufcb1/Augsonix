package cli

import (
	"strings"
	"testing"
)

func TestParseSearchArgsJoinsTheQuery(t *testing.T) {
	// La consulta puede llegar en varias palabras sueltas si quien llama no la
	// entrecomilló. Perderlas dejaría una búsqueda distinta de la que se pidió.
	opts, query, err := parseSearchArgs([]string{"donde", "se", "valida", "el", "token"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "donde se valida el token" {
		t.Errorf("consulta = %q", query)
	}
	if opts.limit != searchDefaultLimit {
		t.Errorf("límite por defecto = %d", opts.limit)
	}
}

func TestParseSearchArgsReadsTheFlags(t *testing.T) {
	// Las banderas se mezclan con la consulta en cualquier orden, porque quien
	// escribe en la terminal no recuerda cuál va primero.
	opts, query, err := parseSearchArgs([]string{"--limit", "3", "cobro", "--json", "duplicado", "--path", "app/"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "cobro duplicado" {
		t.Errorf("consulta = %q", query)
	}
	if opts.limit != 3 || !opts.asJSON || opts.path != "app/" {
		t.Errorf("banderas mal leídas: %+v", opts)
	}
}

func TestParseSearchArgsRejectsWhatItCannotHonor(t *testing.T) {
	// Una opción mal escrita no puede tomarse por parte de la consulta: la
	// búsqueda saldría distinta y nadie entendería por qué.
	cases := map[string][]string{
		"sin consulta":     {"--json"},
		"consulta vacía":   {"   "},
		"límite no número": {"--limit", "muchos", "algo"},
		"límite cero":      {"--limit", "0", "algo"},
		"opción inventada": {"--profundidad", "algo"},
	}
	for name, args := range cases {
		if _, _, err := parseSearchArgs(args); err == nil {
			t.Errorf("%s: se aceptó %v", name, args)
		}
	}
}

func TestParseSearchArgsSelectsTheHistory(t *testing.T) {
	// Buscar en la historia es otro índice, no otra manera de buscar en el
	// código: la bandera tiene que llegar hasta quien elige a cuál preguntar.
	opts, query, err := parseSearchArgs([]string{"--commits", "por qué cambió el troceo"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.commits {
		t.Error("no se reconoció --commits")
	}
	if query != "por qué cambió el troceo" {
		t.Errorf("consulta = %q", query)
	}
}

func TestSearchHitsCarryEnoughToOpenTheFile(t *testing.T) {
	// Quien consuma el JSON necesita poder abrir el archivo en el punto exacto;
	// un resultado sin líneas obliga a buscar a mano lo que ya se encontró.
	hits := codeResultsJSON(nil)
	if len(hits) != 0 {
		t.Fatalf("sin resultados se produjeron %d entradas", len(hits))
	}
	got := commitResultsJSON(nil)
	if len(got) != 0 {
		t.Fatalf("sin commits se produjeron %d entradas", len(got))
	}
}

func TestSearchUsageMentionsTheNewCommand(t *testing.T) {
	// Si el comando no aparece en la ayuda, nadie lo encuentra: es la única
	// pista de que el índice se puede consultar desde fuera del agente.
	if !strings.Contains(searchUsageLine, "search") {
		t.Errorf("la ayuda no menciona search: %q", searchUsageLine)
	}
}
