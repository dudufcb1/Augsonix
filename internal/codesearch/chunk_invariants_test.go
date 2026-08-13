package codesearch

import (
	"fmt"
	"strings"
	"testing"
)

// assertChunkInvariants comprueba las tres garantías de las que depende que un
// resultado de búsqueda sea confiable: el texto de un bloque es exactamente las
// líneas que dice, los bloques no se pisan, y entre todos cubren el archivo.
func assertChunkInvariants(t *testing.T, path, content string) []Chunk {
	t.Helper()
	chunks := ChunkFile(path, content)
	lines := splitLines(content)
	prevEnd := 0
	for i, c := range chunks {
		if c.StartLine < 1 || c.EndLine > len(lines) || c.EndLine < c.StartLine {
			t.Fatalf("%s bloque %d: rango %d-%d fuera del archivo (%d líneas)",
				path, i, c.StartLine, c.EndLine, len(lines))
		}
		if c.StartLine <= prevEnd {
			t.Fatalf("%s bloque %d: arranca en %d y el anterior terminó en %d",
				path, i, c.StartLine, prevEnd)
		}
		if want := joinLines(lines[c.StartLine-1 : c.EndLine]); c.Content != want {
			t.Fatalf("%s bloque %d (%d-%d): el texto no corresponde a esas líneas",
				path, i, c.StartLine, c.EndLine)
		}
		prevEnd = c.EndLine
	}
	return chunks
}

func TestChunkFileNeverMisreportsItsLines(t *testing.T) {
	// El resultado de una búsqueda dice "archivo:inicio-fin" y el agente edita
	// a partir de eso. Si el texto no fuera el de esas líneas exactas, editaría
	// el lugar equivocado. Se cubre cada lenguaje con estructura reconocida.
	cases := map[string]string{
		"a.go":  goSample,
		"a.py":  pythonSample,
		"a.php": phpSample,
		"a.ts":  tsSample,
	}
	for path, content := range cases {
		t.Run(path, func(t *testing.T) {
			if got := assertChunkInvariants(t, path, content); len(got) == 0 {
				t.Fatalf("no se produjo ningún bloque")
			}
		})
	}
}

func TestChunkFileCoversEveryLine(t *testing.T) {
	// Ninguna línea puede quedar fuera del índice: si un tramo se perdiera,
	// habría código imposible de encontrar y nadie se enteraría.
	for path, content := range map[string]string{
		"a.go": goSample, "a.py": pythonSample, "a.php": phpSample, "a.ts": tsSample,
	} {
		chunks := ChunkFile(path, content)
		lines := splitLines(content)
		covered := make([]bool, len(lines)+1)
		for _, c := range chunks {
			for i := c.StartLine; i <= c.EndLine; i++ {
				covered[i] = true
			}
		}
		for i := 1; i <= len(lines); i++ {
			if !covered[i] && strings.TrimSpace(lines[i-1]) != "" {
				t.Errorf("%s: la línea %d (%q) quedó fuera del índice",
					path, i, strings.TrimSpace(lines[i-1]))
			}
		}
	}
}

func TestChunkFileKeepsDeclarationsWhole(t *testing.T) {
	// El objetivo del troceo por estructura: una función que cabe en un bloque
	// no debe quedar repartida entre dos.
	chunks := ChunkFile("a.py", pythonSample)
	for _, want := range []string{"def money(", "def quote_plan(", "class Wallet"} {
		found := false
		for _, c := range chunks {
			if strings.Contains(c.Content, want) && strings.Count(c.Content, "\n") > 1 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q no quedó completa en ningún bloque", want)
		}
	}
}

func TestChunkFileFallsBackWhenStructureIsUnrecognized(t *testing.T) {
	// Un archivo de un lenguaje sin detector, o con sintaxis que el escáner no
	// entiende, se trocea por caracteres como siempre. Nunca se queda sin
	// indexar por no haberle encontrado estructura.
	content := strings.Repeat("una linea de texto plano cualquiera\n", 200)
	for _, path := range []string{"notas.txt", "datos.yaml", "roto.go"} {
		if got := assertChunkInvariants(t, path, content); len(got) == 0 {
			t.Errorf("%s: no se indexó nada", path)
		}
	}
}

func TestChunkFileHandlesHostileSyntax(t *testing.T) {
	// Las trampas que descarrilan a un escáner de llaves o de sangría: un
	// paréntesis en una cadena, una llave en una expresión regular, un heredoc
	// con llaves sueltas. Ninguna debe producir bloques que mientan.
	for path, content := range map[string]string{
		"h.py":  hostilePython,
		"h.js":  hostileJS,
		"h.php": hostilePHP,
	} {
		t.Run(path, func(t *testing.T) {
			assertChunkInvariants(t, path, content)
		})
	}
}

func TestChunkFileIsDeterministic(t *testing.T) {
	// El mismo archivo produce los mismos hashes en corridas distintas: de eso
	// depende que reindexar no vuelva a embeber lo que no cambió.
	for range 3 {
		a := ChunkFile("a.py", pythonSample)
		b := ChunkFile("a.py", pythonSample)
		if len(a) != len(b) {
			t.Fatalf("distinto número de bloques entre corridas: %d y %d", len(a), len(b))
		}
		for j := range a {
			if a[j].Hash != b[j].Hash {
				t.Fatalf("bloque %d cambió de hash entre corridas", j)
			}
		}
	}
}

func TestChunkFileRespectsSizeCeiling(t *testing.T) {
	// Un bloque no puede exceder el techo salvo cuando una sola línea ya lo
	// rebasa, porque partir a media línea produce fragmentos ilegibles.
	for path, content := range map[string]string{
		"a.go": goSample, "a.py": pythonSample, "a.php": phpSample, "a.ts": tsSample,
	} {
		for _, c := range ChunkFile(path, content) {
			if len(c.Content) > maxStructuralChars && strings.Count(c.Content, "\n") > 1 {
				t.Errorf("%s bloque %d-%d mide %d caracteres, techo %d",
					path, c.StartLine, c.EndLine, len(c.Content), maxStructuralChars)
			}
		}
	}
}

// bigDeclaration genera una función más grande que el techo, para ejercitar el
// único camino que puede partir una declaración.
func bigDeclaration() string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc grande() {\n")
	for i := range 200 {
		fmt.Fprintf(&b, "\tresultado := calcular(alfa, beta, gama, delta, %d)\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

func TestChunkFileSplitsDeclarationsTooBigToFit(t *testing.T) {
	// Una función que no cabe en ningún bloque se parte por líneas, pero sigue
	// cubriendo el archivo entero y reportando sus rangos con exactitud.
	chunks := assertChunkInvariants(t, "grande.go", bigDeclaration())
	if len(chunks) < 2 {
		t.Fatalf("esperaba varios bloques para una función gigante, hubo %d", len(chunks))
	}
}
