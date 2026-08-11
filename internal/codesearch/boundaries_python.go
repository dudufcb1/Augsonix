package codesearch

import "strings"

// pythonTopLevel y pythonNested son los niveles de sangría donde se buscan
// declaraciones: el archivo y el cuerpo de una clase con sangría de cuatro.
const (
	pythonTopLevel = 0
	pythonNested   = 4
)

// pythonBoundaries usa la sangría, que en Python es la estructura misma.
func pythonBoundaries(content string) []Boundary {
	lines := pythonStrip(content)
	return attachSubs(pythonCollect(lines, pythonTopLevel), pythonCollect(lines, pythonNested))
}

// pythonCollect recorre las líneas ya sin cadenas ni comentarios y cierra cada
// declaración cuando la sangría vuelve al nivel donde empezó.
func pythonCollect(lines []string, level int) []Boundary {
	var out []Boundary
	open, depth := -1, 0
	for i, l := range lines {
		pending := depth
		depth = nextParenDepth(depth, l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		// Un decorador abre la declaración y la definición que sigue es parte
		// de ella: cerrar ahí dejaría el @decorador como fragmento suelto.
		if open >= 0 && onlyDecorators(lines, open, i) {
			continue
		}
		if open >= 0 && i != open && pending == 0 && pythonIndent(l) <= level {
			out = append(out, Boundary{Start: open + 1, End: lastNonBlank(lines, open, i)})
			open = -1
		}
		if open < 0 && pythonIndent(l) == level && startsDeclaration(l) {
			open = i
		}
	}
	if open >= 0 {
		out = append(out, Boundary{Start: open + 1, End: len(lines)})
	}
	return out
}

// nextParenDepth lleva la cuenta de paréntesis y corchetes abiertos: mientras
// queden pendientes, una firma sigue en la línea siguiente y la sangría no dice
// dónde termina la declaración.
func nextParenDepth(depth int, line string) int {
	depth += strings.Count(line, "(") - strings.Count(line, ")")
	depth += strings.Count(line, "[") - strings.Count(line, "]")
	if depth < 0 {
		return 0
	}
	return depth
}

func startsDeclaration(line string) bool {
	t := strings.TrimSpace(line)
	for _, p := range []string{"def ", "async def ", "class ", "@"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// onlyDecorators indica si de la línea de apertura a la actual solo van
// decoradores, es decir que la definición real todavía no empieza.
func onlyDecorators(lines []string, open, cur int) bool {
	seen := false
	for i := open; i < cur && i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "@") {
			return false
		}
		seen = true
	}
	return seen
}

func pythonIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func lastNonBlank(lines []string, from, before int) int {
	end := before
	for end > from && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

// pythonStrip sustituye por espacios el contenido de cadenas y comentarios,
// conservando sangría y longitud. Sin esto, un paréntesis dentro de un texto
// —print("(")— desbalancea el archivo entero y todas las declaraciones
// siguientes se funden en una sola.
func pythonStrip(content string) []string {
	lines := splitLines(content)
	out := make([]string, len(lines))
	var triple string
	for i, l := range lines {
		b := []byte(l)
		for j := 0; j < len(b); {
			j = stripAt(b, j, &triple)
		}
		out[i] = string(b)
	}
	return out
}

// stripAt borra el tramo que empieza en j si es cadena o comentario y devuelve
// la posición siguiente. triple lleva la comilla triple pendiente entre líneas.
func stripAt(b []byte, j int, triple *string) int {
	if *triple != "" {
		if j+2 < len(b) && string(b[j:j+3]) == *triple {
			blank(b, j, j+3)
			*triple = ""
			return j + 3
		}
		b[j] = ' '
		return j + 1
	}
	switch {
	case b[j] == '#':
		blank(b, j, len(b))
		return len(b)
	case j+2 < len(b) && (string(b[j:j+3]) == `"""` || string(b[j:j+3]) == "'''"):
		*triple = string(b[j : j+3])
		blank(b, j, j+3)
		return j + 3
	case b[j] == '"' || b[j] == '\'':
		return blankQuoted(b, j)
	}
	return j + 1
}

// blankQuoted borra una cadena de una línea desde su comilla de apertura.
func blankQuoted(b []byte, j int) int {
	quote := b[j]
	b[j] = ' '
	for j++; j < len(b); {
		if b[j] == '\\' {
			blank(b, j, min(j+2, len(b)))
			j += 2
			continue
		}
		c := b[j]
		b[j] = ' '
		j++
		if c == quote {
			break
		}
	}
	return j
}

func blank(b []byte, from, to int) {
	for i := from; i < to && i < len(b); i++ {
		b[i] = ' '
	}
}
