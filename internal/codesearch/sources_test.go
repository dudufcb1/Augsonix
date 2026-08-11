package codesearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexableAcceptsSourceAndRejectsTheRest(t *testing.T) {
	// Solo entran extensiones de código y documentación; un binario o una
	// imagen no producen un embedding aprovechable.
	cases := []struct {
		path string
		size int64
		want bool
	}{
		{"internal/agent/agent.go", 4000, true},
		{"src/App.tsx", 4000, true},
		{"README.md", 4000, true},
		{"logo.png", 4000, false},
		{"reasonix", 4000, false},
		{"vacio.go", 0, false},
		{"enorme.go", maxIndexableFileSize + 1, false},
	}
	for _, c := range cases {
		if got := Indexable(c.path, c.size); got != c.want {
			t.Errorf("Indexable(%q, %d) = %v, esperaba %v", c.path, c.size, got, c.want)
		}
	}
}

func TestIndexableIsCaseInsensitive(t *testing.T) {
	// Proyectos viejos de C# y VB traen extensiones en mayúsculas y deben
	// entrar igual al índice.
	if !Indexable("Program.CS", 100) {
		t.Error("una extensión en mayúsculas quedó fuera del índice")
	}
}

func TestMatcherSkipsVendorAndHiddenDirs(t *testing.T) {
	// node_modules y las carpetas ocultas son código ajeno o metadatos: llenan
	// el índice de ruido que el agente nunca va a editar.
	m := newMatcher(t.TempDir())
	for _, name := range []string{"node_modules", "dist", ".git", ".venv"} {
		if !m.skipDir(name, name) {
			t.Errorf("skipDir(%q) = false, esperaba saltarlo", name)
		}
	}
	if m.skipDir("internal", "internal") {
		t.Error("una carpeta normal del proyecto se saltó")
	}
}

func TestMatcherHonorsGitignore(t *testing.T) {
	// Lo que el proyecto ya declaró ignorado no debe indexarse: suele ser
	// generado, y además el usuario ya dijo que no le importa.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("generated/\n*.gen.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newMatcher(dir)
	if !m.skipDir("generated", "generated") {
		t.Error("una carpeta listada en .gitignore no se saltó")
	}
	if !m.skipFile("internal/api.gen.go", "api.gen.go") {
		t.Error("un archivo que cae en un patrón del .gitignore no se saltó")
	}
	if m.skipFile("internal/api.go", "api.go") {
		t.Error("un archivo normal se saltó por el .gitignore")
	}
}

func TestIndexableSkipsGeneratedFiles(t *testing.T) {
	// Un package-lock.json ocupaba 55 fragmentos en un proyecto real: cuota
	// gastada en algo que nadie busca por significado, y ruido en los
	// resultados de todas las demás consultas.
	for _, p := range []string{
		"frontend/package-lock.json",
		"frontend/src/types/api.generated.ts",
		"static/app.min.js",
		"static/app.min.css",
		"internal/api/service.pb.go",
		"internal/api/types_generated.go",
		"go.sum",
		"yarn.lock",
	} {
		if Indexable(p, 4000) {
			t.Errorf("se indexó un archivo generado: %s", p)
		}
	}
}

func TestIndexableKeepsHandWrittenFiles(t *testing.T) {
	// El filtro va por nombre, así que no puede llevarse por delante código
	// escrito a mano que casualmente mencione esas palabras.
	for _, p := range []string{
		"package.json",
		"internal/generator/generator.go",
		"src/lib/mapper.ts",
		"internal/codesearch/sources.go",
		"docs/locking.md",
	} {
		if !Indexable(p, 4000) {
			t.Errorf("se descartó código escrito a mano: %s", p)
		}
	}
}
