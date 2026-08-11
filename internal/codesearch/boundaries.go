package codesearch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// Boundary es el tramo de líneas que ocupa una declaración completa: una
// función, una clase, un método. Las líneas van 1-indexadas.
type Boundary struct {
	Start, End int
	// Sub son las declaraciones anidadas, para poder bajar de nivel cuando una
	// clase entera no cabe en un bloque.
	Sub []Boundary
}

// boundaries localiza las declaraciones de un archivo para que el troceo caiga
// entre ellas y no a media función. Devuelve nil cuando el lenguaje no se
// reconoce o cuando lo detectado no supera la validación: en ese caso el
// troceo por caracteres sigue siendo el comportamiento.
func boundaries(path, content string) []Boundary {
	var bs []Boundary
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		bs = goBoundaries(path, content)
	case ".php", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".java", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rs", ".swift", ".kt", ".scala":
		bs = braceBoundaries(content)
	case ".py":
		bs = pythonBoundaries(content)
	default:
		return nil
	}
	if !validBoundaries(bs, strings.Count(content, "\n")+1) {
		return nil
	}
	return bs
}

// validBoundaries rechaza en bloque una detección incoherente. Un escáner por
// llaves o por sangría puede descarrilar ante sintaxis que no previó, y un
// tramo corrido produciría fragmentos que no corresponden a lo que dicen
// contener. Ante la duda se descarta todo y se trocea por caracteres.
func validBoundaries(bs []Boundary, lines int) bool {
	prevEnd := 0
	for _, b := range bs {
		if b.Start < 1 || b.End < b.Start || b.End > lines || b.Start <= prevEnd {
			return false
		}
		subEnd := b.Start
		for _, s := range b.Sub {
			if s.Start <= subEnd || s.End < s.Start || s.End > b.End {
				return false
			}
			subEnd = s.End
		}
		prevEnd = b.End
	}
	return len(bs) > 0
}

// goBoundaries usa el parser de la biblioteca estándar: es exacto y no cuesta
// una dependencia porque ya viene con la toolchain.
func goBoundaries(path, content string) []Boundary {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil
	}
	out := make([]Boundary, 0, len(f.Decls))
	for _, decl := range f.Decls {
		start := fset.Position(decl.Pos()).Line
		if doc := declDoc(decl); doc != nil {
			start = fset.Position(doc.Pos()).Line
		}
		out = append(out, Boundary{Start: start, End: fset.Position(decl.End()).Line})
	}
	return out
}

// declDoc devuelve el comentario de documentación de una declaración: explica
// qué hace, así que se recupera junto con ella y no como fragmento suelto.
func declDoc(decl ast.Decl) *ast.CommentGroup {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}
	return nil
}

// attachSubs cuelga cada declaración anidada de la que la contiene y descarta
// las de nivel superior que se pisan entre sí.
func attachSubs(top, subs []Boundary) []Boundary {
	sortByStart(top)
	sortByStart(subs)
	top = dropOverlaps(top)
	for i := range top {
		for _, s := range subs {
			if s.Start > top[i].Start && s.End <= top[i].End {
				top[i].Sub = append(top[i].Sub, s)
			}
		}
		top[i].Sub = dropOverlaps(top[i].Sub)
	}
	return top
}

func sortByStart(bs []Boundary) {
	for i := 1; i < len(bs); i++ {
		for j := i; j > 0 && bs[j].Start < bs[j-1].Start; j-- {
			bs[j], bs[j-1] = bs[j-1], bs[j]
		}
	}
}

// dropOverlaps deja solo tramos disjuntos, quedándose con el que empezó antes.
func dropOverlaps(bs []Boundary) []Boundary {
	out := bs[:0]
	last := 0
	for _, b := range bs {
		if b.Start <= last {
			continue
		}
		out = append(out, b)
		last = b.End
	}
	return out
}
