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

// Indexable reporta si un archivo entra al índice por su nombre y tamaño.
func Indexable(path string, size int64) bool {
	if size <= 0 || size > maxIndexableFileSize {
		return false
	}
	return indexableExtensions[strings.ToLower(filepath.Ext(path))]
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
