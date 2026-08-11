package codesearch

// maxStructuralChars es el techo del troceo por declaraciones. Es menor que el
// del troceo por caracteres a propósito: cortando en fronteras reales, un
// bloque chico contiene una o dos declaraciones completas en vez de media
// docena revueltas, y el embedding representa una idea y no un promedio.
const maxStructuralChars = 2000

// structuralChunks arma bloques respetando las fronteras de declaración. Los
// tramos se emiten en orden y contiguos, así que el resultado cubre el archivo
// completo sin huecos ni traslapes.
func structuralChunks(path string, lines []string, bs []Boundary) []Chunk {
	p := &packer{path: path, lines: lines}
	prevEnd := 0
	for _, b := range bs {
		if b.Start > prevEnd+1 {
			p.add(prevEnd+1, b.Start-1)
		}
		p.addDeclaration(b)
		prevEnd = b.End
	}
	if prevEnd < len(lines) {
		p.add(prevEnd+1, len(lines))
	}
	p.flush()
	return p.out
}

// packer acumula tramos contiguos y cierra un bloque cuando el siguiente ya no
// cabe. El estado que lleva es el tramo abierto: dónde empieza, dónde va y
// cuánto mide.
type packer struct {
	path       string
	lines      []string
	out        []Chunk
	start, end int
	length     int
}

// addDeclaration mete una declaración entera si cabe; si no, baja a sus
// métodos, y solo parte por líneas cuando ni siquiera esos caben.
func (p *packer) addDeclaration(b Boundary) {
	if p.size(b.Start, b.End) <= maxStructuralChars {
		p.add(b.Start, b.End)
		return
	}
	if len(b.Sub) == 0 {
		p.splitByLines(b.Start, b.End)
		return
	}
	last := b.Start - 1
	for _, s := range b.Sub {
		if s.Start > last+1 {
			p.add(last+1, s.Start-1)
		}
		if p.size(s.Start, s.End) <= maxStructuralChars {
			p.add(s.Start, s.End)
		} else {
			p.splitByLines(s.Start, s.End)
		}
		last = s.End
	}
	if b.End > last {
		p.add(last+1, b.End)
	}
}

func (p *packer) add(start, end int) {
	n := p.size(start, end)
	if p.length > 0 && p.length+n > maxStructuralChars {
		p.flush()
	}
	if p.length == 0 {
		p.start = start
	}
	p.end, p.length = end, p.length+n
}

// splitByLines es el único camino que puede partir una declaración: se usa
// cuando una sola no cabe en un bloque ni por sus métodos.
func (p *packer) splitByLines(start, end int) {
	for i := start; i <= end && i <= len(p.lines); i++ {
		p.add(i, i)
		if p.length >= maxStructuralChars {
			p.flush()
		}
	}
}

// flush cierra el tramo abierto. Un residuo por debajo del mínimo se pega al
// bloque anterior en vez de descartarse: perderlo dejaría líneas del archivo
// fuera del índice.
func (p *packer) flush() {
	if p.length == 0 {
		return
	}
	defer func() { p.start, p.end, p.length = 0, 0, 0 }()
	if p.length < minChunkChars && len(p.out) > 0 {
		prev := &p.out[len(p.out)-1]
		prev.EndLine = p.end
		prev.Content = p.text(prev.StartLine, p.end)
		prev.Hash = segmentHash(p.path, prev.StartLine, p.end, prev.Content)
		return
	}
	if p.length < minChunkChars {
		return
	}
	content := p.text(p.start, p.end)
	p.out = append(p.out, Chunk{
		Path:      p.path,
		StartLine: p.start,
		EndLine:   p.end,
		Content:   content,
		Hash:      segmentHash(p.path, p.start, p.end, content),
	})
}

func (p *packer) text(start, end int) string {
	return joinLines(p.lines[start-1 : end])
}

func (p *packer) size(start, end int) int {
	n := 0
	for i := start - 1; i < end && i < len(p.lines); i++ {
		n += len(p.lines[i]) + 1
	}
	return n
}
