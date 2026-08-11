package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// toolOutputPreviewLines es cuánto se muestra de un resultado antes de cortar.
// Suficiente para ver qué encontró la herramienta sin tapar la conversación:
// el resultado completo ya lo recibió el modelo.
const toolOutputPreviewLines = 12

// toolOutputSilent son las herramientas que no vale la pena mostrar. bash tiene
// su propio bloque en vivo con Ctrl+B, y las de escritura ya se ven como diff.
var toolOutputSilent = map[string]bool{
	"bash": true, "bash_output": true, "kill_shell": true,
	"write_file": true, "edit_file": true, "multi_edit": true,
	"notebook_edit": true, "delete_range": true, "delete_symbol": true,
	"move_file": true, "todo_write": true,
}

// toolOutputBlock arma el bloque que muestra lo que devolvió una herramienta,
// o nil si no hay nada que enseñar. Sirve para ver qué está leyendo el agente y
// juzgar si le sirvió, que es lo que un resultado silencioso no deja hacer.
func toolOutputBlock(name, output string, width int) []string {
	if toolOutputSilent[name] || strings.TrimSpace(output) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	shown := min(len(lines), toolOutputPreviewLines)

	out := make([]string, 0, shown+1)
	for _, ln := range lines[:shown] {
		out = append(out, dim(clampPlain(ln, max(width-4, 20))))
	}
	if len(lines) > shown {
		out = append(out, dim(fmt.Sprintf("… %d more lines", len(lines)-shown)))
	}
	return out
}

// commitToolOutput escribe en el historial lo que devolvió una herramienta,
// cuando el usuario pidió verlo. Sin esto un resultado correcto es invisible y
// no hay forma de juzgar si a la herramienta le sirvió lo que encontró.
func (m *chatTUI) commitToolOutput(tc event.Tool) {
	if !m.showToolOutput || tc.Err != "" {
		return
	}
	block := toolOutputBlock(tc.Name, tc.Output, m.width)
	if block == nil {
		return
	}
	m.finalizeStreamed()
	m.commitLine(connectorBlock(block))
}

// toggleOutputDetail enruta las dos teclas que revelan salida: Ctrl+B expande
// el último bloque de shell y Ctrl+T muestra lo que devuelven las tools de
// lectura. Van juntas porque responden a la misma pregunta: qué pasó de verdad.
func (m *chatTUI) toggleOutputDetail(key string) {
	if key == "ctrl+b" {
		m.toggleShellOutput()
		return
	}
	m.toggleToolOutput()
}

// toggleToolOutput enciende o apaga la vista de resultados. Solo afecta a las
// llamadas siguientes: lo ya escrito en el historial no se reescribe.
func (m *chatTUI) toggleToolOutput() {
	m.showToolOutput = !m.showToolOutput
	if m.showToolOutput {
		m.notice(i18n.M.ChatToolOutputOn)
		return
	}
	m.notice(i18n.M.ChatToolOutputOff)
}

// showsToolOutputFor reporta si el contenido de esa herramienta se va a pintar
// completo. Cuando sí, el resumen "⎿ N lines" sobra: diría el conteo justo
// encima de las líneas que se están mostrando.
func (m *chatTUI) showsToolOutputFor(name string) bool {
	return m.showToolOutput && name != "" && !toolOutputSilent[name]
}

// dropToolStream cierra el bloque en vivo sin dejar resumen, para que el
// contenido que viene después ocupe su lugar en vez de sumarse.
func (m *chatTUI) dropToolStream(id string) {
	if id != "" && m.toolStreamID == id {
		m.toolStreamIdx = -1
		m.toolStreamID = ""
		m.toolTail = m.toolTail[:0]
		m.toolPartial = ""
		m.toolLineCount = 0
	}
}
