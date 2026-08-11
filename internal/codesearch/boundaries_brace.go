package codesearch

import "strings"

// Estados del recorrido: una llave dentro de una cadena o de un comentario no
// es estructura, así que hay que saber en qué contexto va cada carácter.
const (
	braceCode = iota
	braceLineComment
	braceBlockComment
	braceSingle
	braceDouble
	braceTemplate
)

// braceBoundaries deduce la estructura contando llaves balanceadas. Sirve para
// la familia C: PHP, TypeScript, JavaScript, Java, Rust.
func braceBoundaries(content string) []Boundary {
	lines := splitLines(content)
	s := braceScanner{src: content, lines: lines, line: 1}
	s.run()
	return attachSubs(s.top, s.subs)
}

type braceOpen struct{ line, depth int }

type braceScanner struct {
	src   string
	lines []string
	line  int
	depth int
	state int
	stack []braceOpen
	top   []Boundary
	subs  []Boundary
	// lastSig y lastWord dan el contexto que decide si una barra abre una
	// expresión regular o divide.
	lastSig  byte
	lastWord string
	word     []byte
}

func (s *braceScanner) run() {
	for i := 0; i < len(s.src); i++ {
		c := s.src[i]
		if c == '\n' {
			s.line++
			if s.state == braceLineComment {
				s.state = braceCode
			}
			continue
		}
		if s.state != braceCode {
			i = s.inSpan(i, c)
			continue
		}
		i = s.inCode(i, c)
		s.track(c)
	}
}

// inSpan avanza dentro de una cadena o comentario hasta su cierre.
func (s *braceScanner) inSpan(i int, c byte) int {
	switch s.state {
	case braceLineComment:
		return i
	case braceBlockComment:
		if c == '*' && i+1 < len(s.src) && s.src[i+1] == '/' {
			s.state = braceCode
			return i + 1
		}
	case braceSingle, braceDouble, braceTemplate:
		if c == '\\' {
			return i + 1
		}
		if (s.state == braceSingle && c == '\'') ||
			(s.state == braceDouble && c == '"') ||
			(s.state == braceTemplate && c == '`') {
			s.state = braceCode
		}
	}
	return i
}

func (s *braceScanner) inCode(i int, c byte) int {
	switch c {
	case '/':
		return s.slash(i)
	case '<':
		if strings.HasPrefix(s.src[i:], "<<<") {
			if ni, nl, ok := skipHeredoc(s.src, i, s.line); ok {
				s.line = nl
				return ni
			}
		}
	case '#':
		s.state = braceLineComment // comentario de PHP
	case '\'':
		s.state = braceSingle
	case '"':
		s.state = braceDouble
	case '`':
		s.state = braceTemplate
	case '{':
		s.stack = append(s.stack, braceOpen{line: s.line, depth: s.depth})
		s.depth++
	case '}':
		s.close()
	}
	return i
}

func (s *braceScanner) slash(i int) int {
	if i+1 < len(s.src) {
		switch s.src[i+1] {
		case '/':
			s.state = braceLineComment
			return i + 1
		case '*':
			s.state = braceBlockComment
			return i + 1
		}
	}
	if regexAllowed(s.lastSig, s.lastWord) {
		return skipRegex(s.src, i)
	}
	return i
}

func (s *braceScanner) close() {
	if s.depth == 0 || len(s.stack) == 0 {
		return
	}
	s.depth--
	o := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	// Un bloque que abre y cierra en la misma línea es un objeto o un tipo
	// escrito en línea, no el cuerpo de una declaración.
	if s.line <= o.line {
		return
	}
	b := Boundary{Start: declStart(s.lines, o.line), End: s.line}
	switch o.depth {
	case 0:
		s.top = append(s.top, b)
	case 1:
		s.subs = append(s.subs, b)
	}
}

func (s *braceScanner) track(c byte) {
	if c > ' ' {
		s.lastSig = c
	}
	if isWordByte(c) {
		s.word = append(s.word, c)
		return
	}
	if len(s.word) > 0 {
		s.lastWord = string(s.word)
		s.word = s.word[:0]
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// declStart retrocede desde la llave de apertura hasta el principio real de la
// declaración: firmas partidas en varias líneas, decoradores y el comentario
// que las documenta.
func declStart(lines []string, braceLine int) int {
	start := braceLine
	for start > 1 {
		prev := strings.TrimSpace(lines[start-2])
		// Un "{" al final de la línea anterior abre el bloque que nos contiene;
		// cruzarlo haría que un método arrancara en su clase.
		if prev == "" || strings.HasSuffix(prev, "}") ||
			strings.HasSuffix(prev, ";") || strings.HasSuffix(prev, "{") {
			break
		}
		start--
	}
	for start > 1 && isAttachedComment(strings.TrimSpace(lines[start-2])) {
		start--
	}
	return start
}

func isAttachedComment(line string) bool {
	for _, p := range []string{"//", "*", "/*", "#", "@"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// regexAllowed decide si una barra abre una expresión regular o divide. En
// JavaScript solo lo dice el contexto: tras un valor divide, tras un operador o
// al inicio de una sentencia abre un literal.
func regexAllowed(prev byte, prevWord string) bool {
	if prev == 0 {
		return true
	}
	switch prev {
	case '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '~', '^', '<', '>', '%':
		return true
	}
	switch prevWord {
	case "return", "typeof", "instanceof", "in", "of", "new", "delete",
		"void", "throw", "case", "do", "else", "yield", "await":
		return true
	}
	return false
}

// skipRegex consume un literal de expresión regular, contando las clases [...]
// para que una barra dentro de ellas no lo cierre antes de tiempo.
func skipRegex(src string, i int) int {
	inClass := false
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '\n':
			return i // sin cierre en la línea: no era una expresión regular
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return j
			}
		}
	}
	return i
}

// skipHeredoc salta un bloque <<<EOT de PHP, cuyo contenido es texto libre y
// puede llevar llaves desbalanceadas.
func skipHeredoc(src string, i, line int) (int, int, bool) {
	j := i + 3
	for j < len(src) && (src[j] == ' ' || src[j] == '\'' || src[j] == '"') {
		j++
	}
	start := j
	for j < len(src) && (isWordByte(src[j]) || (src[j] >= '0' && src[j] <= '9')) {
		j++
	}
	tag := src[start:j]
	if tag == "" {
		return i, line, false
	}
	for {
		nl := strings.IndexByte(src[j:], '\n')
		if nl < 0 {
			return len(src) - 1, line, true
		}
		j += nl + 1
		line++
		rest := src[j:]
		trimmed := strings.TrimLeft(rest, " \t")
		if closesHeredoc(trimmed, tag) {
			return j + len(rest) - len(trimmed) + len(tag) - 1, line, true
		}
	}
}

func closesHeredoc(line, tag string) bool {
	if !strings.HasPrefix(line, tag) {
		return false
	}
	after := line[len(tag):]
	return after == "" || after[0] == ';' || after[0] == ',' ||
		after[0] == ')' || after[0] == '\n'
}
