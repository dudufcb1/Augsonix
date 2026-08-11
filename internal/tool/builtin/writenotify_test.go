package builtin

import "testing"

func TestEveryWriterPathCarriesTheWriteNotice(t *testing.T) {
	// El aviso se cablea en dos sitios: el Workspace y ConfineWriters, que es la
	// ruta de respaldo cuando no hay directorio de trabajo. Si una se queda sin
	// él, las escrituras de ese camino no llegan al índice y nadie se entera.
	for i, build := range writerToolsNotify {
		var seen []string
		tools := build(func(p string) { seen = append(seen, p) })
		if len(tools) == 0 {
			t.Fatalf("ruta %d no construyó herramientas", i)
		}
		names := map[string]bool{}
		for _, tl := range tools {
			names[tl.Name()] = true
		}
		for _, want := range []string{"write_file", "edit_file", "multi_edit"} {
			if !names[want] {
				t.Errorf("ruta %d no incluye %s", i, want)
			}
		}
	}
}
