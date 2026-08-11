package codesearch

import (
	"strings"
	"testing"
)

func TestChunkFileSkipsContentBelowMinimum(t *testing.T) {
	// Un archivo de dos líneas no llega al mínimo de caracteres, así que no
	// produce bloques: embeber un fragmento así gasta cuota y no aporta señal.
	got := ChunkFile("a.go", "package main\n")
	if len(got) != 0 {
		t.Fatalf("esperaba 0 bloques para contenido diminuto, hubo %d", len(got))
	}
}

func TestChunkFileRecordsLineRange(t *testing.T) {
	// Las líneas se reportan 1-indexadas y cubren el archivo completo, para que
	// un resultado de búsqueda se pueda abrir directo en el editor.
	src := strings.Repeat("const value = compute()\n", 10)
	got := ChunkFile("a.ts", src)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 bloque, hubo %d", len(got))
	}
	if got[0].StartLine != 1 {
		t.Errorf("StartLine = %d, esperaba 1", got[0].StartLine)
	}
	if got[0].EndLine < 10 {
		t.Errorf("EndLine = %d, esperaba cubrir las 10 líneas", got[0].EndLine)
	}
}

func TestChunkFileSplitsOversizedContent(t *testing.T) {
	// Un archivo bastante mayor al máximo se parte en varios bloques y ninguno
	// se pasa del margen de tolerancia.
	src := strings.Repeat("x := compute(alpha, beta, gamma)\n", 600)
	got := ChunkFile("big.go", src)
	if len(got) < 2 {
		t.Fatalf("esperaba varios bloques, hubo %d", len(got))
	}
	for i, c := range got {
		if len(c.Content) > effectiveMaxChars {
			t.Errorf("bloque %d mide %d caracteres, máximo %d", i, len(c.Content), effectiveMaxChars)
		}
	}
}

func TestChunkFileSplitsSingleHugeLine(t *testing.T) {
	// Un bundle minificado es una sola línea de cientos de miles de caracteres:
	// se trocea igual en vez de producir un bloque gigante o ninguno.
	src := strings.Repeat("a", effectiveMaxChars*3)
	got := ChunkFile("bundle.js", src)
	if len(got) < 3 {
		t.Fatalf("esperaba al menos 3 bloques de una línea larga, hubo %d", len(got))
	}
	for i, c := range got {
		if len(c.Content) > effectiveMaxChars {
			t.Errorf("bloque %d mide %d caracteres, máximo %d", i, len(c.Content), effectiveMaxChars)
		}
	}
}

func TestChunkFileHashChangesWithContent(t *testing.T) {
	// El hash de segmento distingue contenido: si el bloque cambia, el índice
	// tiene que notarlo y re-embeberlo.
	a := ChunkFile("a.go", strings.Repeat("alpha := 1\n", 10))
	b := ChunkFile("a.go", strings.Repeat("beta := 2\n", 10))
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("ambos contenidos debían producir bloques")
	}
	if a[0].Hash == b[0].Hash {
		t.Error("dos contenidos distintos produjeron el mismo hash de segmento")
	}
}

func TestChunkFileNormalizesCRLF(t *testing.T) {
	// Un archivo con finales de línea de Windows debe producir los mismos
	// bloques que uno con finales Unix, o cambiar de sistema reindexaría todo.
	unix := ChunkFile("a.go", strings.Repeat("value := 1\n", 10))
	win := ChunkFile("a.go", strings.Repeat("value := 1\r\n", 10))
	if len(unix) != len(win) {
		t.Fatalf("bloques distintos: unix=%d windows=%d", len(unix), len(win))
	}
	if unix[0].Hash != win[0].Hash {
		t.Error("CRLF produjo un hash distinto que LF")
	}
}
