package config

import "strings"

// renderMemory escribe la sección [memory] del config. Igual que [codesearch]
// y [workspace], debe emitirse o el guardado canónico borra el flag y Reasonix
// dejaría de leer las memorias de Claude sin avisar. Se omite en default.
func renderMemory(b *strings.Builder, m MemoryConfig) {
	if !m.ClaudeStore {
		return
	}
	b.WriteString("\n[memory]\n")
	b.WriteString("# Lee las memorias que Claude Code guardó para este proyecto (solo lectura).\n")
	b.WriteString("# Resuelve el home de Claude así: $CLAUDE_CONFIG_DIR, luego ~/.claude-assistant,\n")
	b.WriteString("# luego ~/.claude. Si ninguno tiene memoria para el proyecto, se usa el\n")
	b.WriteString("# store propio de Reasonix.\n")
	b.WriteString("claude_store = true\n")
}
