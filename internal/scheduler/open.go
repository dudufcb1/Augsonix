package scheduler

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/claudesessions"
	"reasonix/internal/schedulable"
)

// terminals son los emuladores probados en orden. Cada uno recibe el comando
// como una sola cadena de shell después de su bandera de ejecución.
var terminals = []struct {
	bin  string
	args func(dir, command string) []string
}{
	{"gnome-terminal", func(dir, cmd string) []string {
		return []string{"--working-directory=" + dir, "--", "bash", "-c", cmd}
	}},
	{"konsole", func(dir, cmd string) []string {
		return []string{"--workdir", dir, "-e", "bash", "-c", cmd}
	}},
	{"xfce4-terminal", func(dir, cmd string) []string {
		return []string{"--working-directory=" + dir, "-x", "bash", "-c", cmd}
	}},
	{"x-terminal-emulator", func(dir, cmd string) []string {
		return []string{"-e", "bash", "-c", cmd}
	}},
	{"xterm", func(dir, cmd string) []string {
		return []string{"-e", "bash", "-c", cmd}
	}},
}

// resumeShellCommand arma la línea que reanuda la sesión en primer plano. El
// "exec bash" final deja la terminal abierta cuando el agente termina, para
// que la ventana no se cierre llevándose lo que quedó en pantalla.
func resumeShellCommand(entry schedulable.Entry) string {
	var b strings.Builder
	if entry.Agent() == schedulable.KindClaude {
		if home := strings.TrimSpace(entry.ConfigHome); claudesessions.NeedsConfigHome(home) {
			fmt.Fprintf(&b, "%s=%s ", claudesessions.ResumeEnv, shellQuote(home))
		}
		fmt.Fprintf(&b, "claude --resume %s", shellQuote(entry.SessionID))
	} else {
		fmt.Fprintf(&b, "reasonix --resume %s", shellQuote(entry.SessionPath))
	}
	b.WriteString("; exec bash")
	return b.String()
}

// resumeShellCommandWith añade el encargo como primer mensaje de la sesión y,
// en Claude Code, el modelo elegido. El prompt va posicional, igual que cuando
// se escribe en la terminal.
func resumeShellCommandWith(entry schedulable.Entry, task schedulable.Task) string {
	base := strings.TrimSuffix(resumeShellCommand(entry), "; exec bash")
	if model := strings.TrimSpace(task.Model); model != "" {
		base += " --model " + shellQuote(model)
	}
	if prompt := strings.TrimSpace(task.Prompt); prompt != "" {
		base += " " + shellQuote(prompt)
	}
	return base + "; exec bash"
}

// shellQuote envuelve en comillas simples, que es lo único que el shell no
// reinterpreta; las comillas internas se cierran y se reabren.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// OpenInTerminalWithPrompt abre la sesión en una ventana y le entrega el
// encargo ya escrito, para que el trabajo arranque solo y el usuario llegue a
// una conversación en curso en vez de a un cursor vacío.
func OpenInTerminalWithPrompt(entry schedulable.Entry, task schedulable.Task) error {
	return openTerminal(entry, resumeShellCommandWith(entry, task))
}

// OpenInTerminal abre la sesión en una ventana nueva para seguir conversando.
func OpenInTerminal(entry schedulable.Entry) error {
	return openTerminal(entry, resumeShellCommand(entry))
}

// launchGrace es cuánto se espera a que el emulador reporte un fallo
// inmediato (sin DISPLAY, argumento inválido). gnome-terminal termina en
// milisegundos tras delegar la ventana a su servidor; xterm sigue vivo mientras
// la ventana exista. Si pasado este plazo sigue corriendo, la ventana abrió.
const launchGrace = 2 * time.Second

func openTerminal(entry schedulable.Entry, command string) error {
	if err := checkAgentOnPath(entry); err != nil {
		return err
	}
	dir := strings.TrimSpace(entry.WorkDir)
	if dir == "" {
		dir = "."
	}
	for _, t := range terminals {
		path, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		return startTerminal(t.bin, path, t.args(dir, command))
	}
	return fmt.Errorf("scheduler: no se encontró ningún emulador de terminal instalado")
}

// checkAgentOnPath confirma que el binario que la ventana va a ejecutar existe
// en el PATH de este proceso. La terminal hereda ese PATH, así que si el
// agente no está aquí, adentro tampoco: la ventana abriría con un "command
// not found" que el panel jamás vería.
func checkAgentOnPath(entry schedulable.Entry) error {
	bin := "reasonix"
	if entry.Agent() == schedulable.KindClaude {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("scheduler: la terminal no encontraría %q (PATH=%s)", bin, os.Getenv("PATH"))
	}
	return nil
}

// startTerminal lanza el emulador y se queda escuchando su stderr un momento:
// si muere enseguida (por ejemplo sin DISPLAY, "no se puede abrir el visor")
// el error llega al panel en vez de perderse en /dev/null con un "abierta".
func startTerminal(name, path string, args []string) error {
	cmd := exec.Command(path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("scheduler: no se pudo abrir %s: %w", name, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("scheduler: %s falló al abrir la ventana (%v): %s",
				name, err, stderrSummary(stderr.String()))
		}
		return nil
	case <-time.After(launchGrace):
		return nil
	}
}

// stderrSummary deja el stderr en una sola línea legible para el panel.
func stderrSummary(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "(sin salida en stderr)"
	}
	return strings.Join(strings.Fields(text), " ")
}
