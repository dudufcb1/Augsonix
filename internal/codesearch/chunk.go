package codesearch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Límites de troceo, heredados del indexador de referencia: un bloque grande
// diluye el embedding y uno chico no dice nada por sí solo.
const (
	maxChunkChars       = 5000
	minChunkChars       = 50
	minRemainderChars   = 500
	chunkToleranceRatio = 1.15
)

// effectiveMaxChars deja cerrar un bloque un poco pasado del máximo antes que
// partir a media declaración.
const effectiveMaxChars = int(maxChunkChars * chunkToleranceRatio)

// Chunk es un fragmento de archivo indexable, con su ubicación exacta para que
// el resultado de una búsqueda se pueda abrir sin volver a buscar.
type Chunk struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
	Hash      string `json:"hash"`
}

// ChunkFile parte el contenido de un archivo en bloques indexables. path se
// guarda tal cual en cada Chunk y participa en su hash, así que debe venir ya
// normalizado (relativo al workspace) para que el mismo archivo produzca los
// mismos hashes entre corridas.
func ChunkFile(path, content string) []Chunk {
	lines := splitLines(content)
	if bs := boundaries(path, content); bs != nil {
		return structuralChunks(path, lines, bs)
	}
	c := chunker{path: path, lines: lines}
	for i := range lines {
		c.appendLine(i)
	}
	c.flush(len(lines) - 1)
	return c.out
}

type chunker struct {
	path    string
	lines   []string
	out     []Chunk
	current []string
	length  int
	start   int
}

func (c *chunker) appendLine(i int) {
	line := c.lines[i]
	lineLen := len(line) + 1
	if lineLen > effectiveMaxChars {
		c.splitLongLine(i, line)
		return
	}
	// Cortar antes de la línea evita dejar una cola huérfana demasiado corta
	// para valer un bloque propio.
	if c.length+lineLen > effectiveMaxChars && len(c.current) >= 1 && c.nextLineLen(i) < minRemainderChars {
		c.flush(i - 1)
	}
	c.current = append(c.current, line)
	c.length += lineLen
	if c.length >= effectiveMaxChars {
		c.flush(i)
	}
}

func (c *chunker) nextLineLen(i int) int {
	if i+1 >= len(c.lines) {
		return 0
	}
	return len(c.lines[i+1])
}

// splitLongLine trocea una sola línea que por sí misma rebasa el máximo, como
// pasa con bundles minificados o data URIs embebidos.
func (c *chunker) splitLongLine(i int, line string) {
	if len(c.current) > 0 {
		c.flush(i - 1)
	}
	for len(line) > 0 {
		cut := min(effectiveMaxChars, len(line))
		c.current = []string{line[:cut]}
		c.length = cut
		c.start = i
		c.flush(i)
		line = line[cut:]
	}
	c.start = i + 1
	c.current = nil
	c.length = 0
}

func (c *chunker) flush(endLine int) {
	defer func() {
		c.current = nil
		c.length = 0
		c.start = endLine + 1
	}()
	if c.length < minChunkChars || len(c.current) == 0 {
		return
	}
	content := strings.Join(c.current, "\n")
	start, end := c.start+1, endLine+1
	c.out = append(c.out, Chunk{
		Path:      c.path,
		StartLine: start,
		EndLine:   end,
		Content:   content,
		Hash:      segmentHash(c.path, start, end, content),
	})
}

// segmentHash identifica un bloque por posición y contenido: si el archivo
// cambia arriba y el bloque se recorre, el hash cambia y se reindexa.
func segmentHash(path string, start, end int, content string) string {
	preview := content
	if len(preview) > 100 {
		preview = preview[:100]
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s-%d-%d-%d-%s", path, start, end, len(content), preview))
	return hex.EncodeToString(sum[:])
}

// FileHash identifica el contenido completo de un archivo, para saltarse los
// que no cambiaron sin volver a trocearlos.
func FileHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}
