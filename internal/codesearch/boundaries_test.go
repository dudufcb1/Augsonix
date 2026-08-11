package codesearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// declarationHead son las líneas del arranque de una frontera donde puede caer
// la declaración: la primera, o unas pocas después si viene precedida de su
// comentario de documentación o de sus decoradores.
const declarationHead = 8

// declarationAt indica si alguna frontera arranca en la declaración buscada,
// sin depender de números de línea que cambian al editar la muestra.
func declarationAt(t *testing.T, path, content, marker string) bool {
	t.Helper()
	lines := splitLines(content)
	headHas := func(b Boundary) bool {
		for i := b.Start; i <= b.End && i < b.Start+declarationHead && i <= len(lines); i++ {
			if strings.Contains(lines[i-1], marker) {
				return true
			}
		}
		return false
	}
	for _, b := range boundaries(path, content) {
		if headHas(b) {
			return true
		}
		for _, s := range b.Sub {
			if headHas(s) {
				return true
			}
		}
	}
	return false
}

func TestBoundariesFindsDeclarationsPerLanguage(t *testing.T) {
	// Cada lenguaje con detector propio debe reconocer sus formas habituales:
	// funciones sueltas, clases y los métodos de adentro.
	cases := []struct {
		path, content string
		markers       []string
	}{
		{"a.go", goSample, []string{"func Normalize", "func (c *Counter) Add"}},
		{"a.py", pythonSample, []string{"def money(", "class WalletBalances"}},
		{"a.php", phpSample, []string{"class FailureRecorder", "public function record",
			"function normalizeAccount"}},
		{"a.ts", tsSample, []string{"export async function searchCode",
			"export class Registry", "add(item: string)"}},
	}
	for _, c := range cases {
		for _, m := range c.markers {
			if !declarationAt(t, c.path, c.content, m) {
				t.Errorf("%s: no se detectó la declaración %q", c.path, m)
			}
		}
	}
}

func TestBoundariesKeepsDocCommentWithDeclaration(t *testing.T) {
	// El comentario que documenta una función explica qué hace, así que se
	// recupera junto con ella en vez de quedar como fragmento sin dueño.
	if !declarationAt(t, "a.go", goSample, "// Normalize deja el texto") {
		t.Error("go: la declaración no absorbió su comentario de documentación")
	}
	if !declarationAt(t, "a.php", phpSample, "/**") {
		t.Error("php: la declaración no absorbió su bloque de documentación")
	}
}

func TestBoundariesKeepsDecoratorWithDefinition(t *testing.T) {
	// Un decorador de Python es parte de la definición que sigue: separarlos
	// deja el @decorador como fragmento suelto y la función sin su contexto.
	if !declarationAt(t, "a.py", pythonSample, "@staticmethod") {
		t.Error("la definición decorada no arrancó en su decorador")
	}
}

func TestBoundariesIgnoresPunctuationInsideText(t *testing.T) {
	// Un paréntesis en una cadena de Python o una llave en una expresión
	// regular de JavaScript no son estructura. Si se contaran, las
	// declaraciones siguientes se fundirían en una sola.
	cases := []struct {
		path, content string
		want          int
	}{
		{"h.py", hostilePython, 3},
		{"h.js", hostileJS, 3},
		{"h.php", hostilePHP, 2},
	}
	for _, c := range cases {
		if got := len(boundaries(c.path, c.content)); got != c.want {
			t.Errorf("%s: %d declaraciones, esperaba %d", c.path, got, c.want)
		}
	}
}

func TestBoundariesReturnsNilForUnknownLanguages(t *testing.T) {
	// Sin detector para la extensión no se inventa estructura: el troceo por
	// caracteres es el comportamiento correcto.
	for _, path := range []string{"notas.txt", "config.yaml", "estilos.css"} {
		if got := boundaries(path, goSample); got != nil {
			t.Errorf("%s: devolvió %d fronteras, esperaba ninguna", path, len(got))
		}
	}
}

func TestBoundariesReturnsNilWhenSourceDoesNotParse(t *testing.T) {
	// Un archivo Go que no compila no da fronteras confiables: mejor caer al
	// troceo por caracteres que cortar según un árbol incompleto.
	if got := boundaries("roto.go", "func sin cerrar( {{{\n"); got != nil {
		t.Errorf("devolvió %d fronteras para código inválido", len(got))
	}
}

func TestValidBoundariesRejectsIncoherentDetections(t *testing.T) {
	// La red de seguridad: cualquier incoherencia descarta la detección
	// completa. Un tramo traslapado o fuera de rango produciría bloques cuyo
	// texto no corresponde al rango que reportan.
	cases := map[string][]Boundary{
		"traslapadas":     {{Start: 1, End: 10}, {Start: 5, End: 20}},
		"desordenadas":    {{Start: 10, End: 20}, {Start: 1, End: 5}},
		"fuera de rango":  {{Start: 1, End: 500}},
		"inicio inválido": {{Start: 0, End: 5}},
		"fin antes":       {{Start: 10, End: 3}},
		"vacía":           {},
		"sub fuera":       {{Start: 1, End: 10, Sub: []Boundary{{Start: 5, End: 40}}}},
	}
	for name, bs := range cases {
		if validBoundaries(bs, 50) {
			t.Errorf("%s: se aceptó una detección que debía rechazarse", name)
		}
	}
	ok := []Boundary{{Start: 1, End: 10, Sub: []Boundary{{Start: 2, End: 4}}}, {Start: 11, End: 20}}
	if !validBoundaries(ok, 50) {
		t.Error("se rechazó una detección coherente")
	}
}

func TestChunkFileInvariantsHoldOnThisPackage(t *testing.T) {
	// La prueba más amplia: el código real de este paquete, con toda su
	// variedad de sintaxis, troceado y verificado contra los invariantes. Es
	// la que atraparía una regresión que las muestras no cubren.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		assertChunkInvariants(t, e.Name(), string(raw))
		checked++
	}
	if checked == 0 {
		t.Fatal("no se revisó ningún archivo")
	}
}
