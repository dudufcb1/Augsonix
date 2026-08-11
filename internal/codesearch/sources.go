package codesearch

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// maxIndexableFileSize deja fuera los archivos que casi siempre son generados
// (bundles, lockfiles enormes, blobs) y no aportan a una búsqueda de código.
const maxIndexableFileSize = 1 << 20

// indexableExtensions son los archivos que vale la pena embeber. Todo lo demás
// se salta sin leerlo: un binario no produce un embedding útil y sí cuesta.
var indexableExtensions = map[string]bool{
	".c": true, ".cpp": true, ".cs": true, ".css": true, ".ejs": true,
	".el": true, ".elm": true, ".erb": true, ".ex": true, ".exs": true,
	".go": true, ".h": true, ".hpp": true, ".htm": true, ".html": true,
	".java": true, ".js": true, ".json": true, ".jsx": true, ".kt": true,
	".kts": true, ".lua": true, ".markdown": true, ".md": true, ".ml": true,
	".mli": true, ".php": true, ".py": true, ".rb": true, ".rdl": true,
	".rs": true, ".scala": true, ".sol": true, ".swift": true, ".tla": true,
	".toml": true, ".ts": true, ".tsx": true, ".vb": true, ".vue": true,
	".zig": true,
}

// skipDirs son carpetas de dependencias y artefactos de build. Indexarlas
// llena el índice de código ajeno que el agente nunca va a editar.
var skipDirs = map[string]bool{
	"Pods": true, "__pycache__": true, "build": true, "bundle": true,
	"deps": true, "dist": true, "env": true, "node_modules": true,
	"out": true, "pkg": true, "target": true, "temp": true, "tmp": true,
	"vendor": true, "venv": true,
}

// generatedSuffixes son archivos que nadie busca por significado: lockfiles,
// bundles minificados y salida de generadores. Se trocean en decenas de
// fragmentos cada uno, así que cuestan cuota y ensucian los resultados.
var generatedSuffixes = []string{
	".min.js", ".min.css", ".map",
	"-lock.json", ".lock",
	".generated.ts", ".generated.js", ".generated.go", "_generated.go", ".gen.go",
	".pb.go",
}

// generatedNames son nombres completos que siempre son generados.
var generatedNames = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"composer.lock": true, "go.sum": true, "cargo.lock": true, "poetry.lock": true,
}

// Indexable reporta si un archivo entra al índice por su nombre y tamaño.
func Indexable(path string, size int64) bool {
	if size <= 0 || size > maxIndexableFileSize {
		return false
	}
	if !indexableExtensions[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	return !isGenerated(path)
}

// isGenerated reconoce salida de herramientas por su nombre. Se mira el nombre
// y no el contenido porque decidirlo leyendo el archivo costaría abrirlos todos.
func isGenerated(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if generatedNames[name] {
		return true
	}
	for _, suffix := range generatedSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// generatedLineAverage es el promedio de caracteres por línea a partir del cual
// un archivo se toma por salida de una herramienta. Código escrito a mano ronda
// las decenas; un bundle o una tabla generada va en centenas o miles.
const generatedLineAverage = 400

// IndexableContent descarta lo que ya se leyó y resultó ser salida de máquina:
// un archivo minificado se trocea a media línea, así que los rangos que
// acompañan a un resultado no señalan lo que el bloque realmente trae. Además
// nadie busca por significado dentro de un bundle.
func IndexableContent(content string) bool {
	lines := strings.Count(content, "\n") + 1
	if len(content)/lines > generatedLineAverage {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		if len(line) > maxChunkChars {
			return false
		}
	}
	return true
}

// matcher decide qué se salta durante el recorrido: carpetas ocultas, las de
// dependencias, y lo que diga el .gitignore de la raíz del workspace.
type matcher struct {
	gi *ignore.GitIgnore
}

func newMatcher(root string) *matcher {
	m := &matcher{}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return m
	}
	m.gi = ignore.CompileIgnoreLines(strings.Split(string(data), "\n")...)
	return m
}

// skipDir reporta si el recorrido no debe entrar a un directorio. rel es su
// ruta relativa a la raíz del workspace, con separadores "/".
func (m *matcher) skipDir(rel, name string) bool {
	if hidden(name) || skipDirs[name] {
		return true
	}
	return m.gi != nil && m.gi.MatchesPath(rel+"/")
}

// skipFile reporta si un archivo queda fuera del índice por nombre o ignore.
func (m *matcher) skipFile(rel, name string) bool {
	if hidden(name) {
		return true
	}
	return m.gi != nil && m.gi.MatchesPath(rel)
}

func hidden(name string) bool { return len(name) > 1 && name[0] == '.' }
