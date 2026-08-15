package config

import "strings"

// renderWorkspace escribe la sección [workspace] del config. Igual que
// [codesearch], debe emitirse o el guardado canónico borra el flag y reactiva
// la serialización de escritores sin avisar. Se omite cuando está en default.
func renderWorkspace(b *strings.Builder, w WorkspaceConfig) {
	if !w.ConcurrentWriters {
		return
	}
	b.WriteString("\n[workspace]\n")
	b.WriteString("# Permite que varias sesiones escriban el workspace a la vez.\n")
	b.WriteString("# Úsalo solo en carpetas contenedoras (muchos proyectos independientes)\n")
	b.WriteString("# cuyas sesiones nunca tocan los mismos archivos.\n")
	b.WriteString("concurrent_writers = true\n")
}
