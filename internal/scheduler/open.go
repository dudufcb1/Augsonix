package scheduler

import (
	"fmt"
	"os/exec"
	"strings"

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

func openTerminal(entry schedulable.Entry, command string) error {
	dir := strings.TrimSpace(entry.WorkDir)
	if dir == "" {
		dir = "."
	}
	for _, t := range terminals {
		path, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, t.args(dir, command)...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("scheduler: no se pudo abrir %s: %w", t.bin, err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return fmt.Errorf("scheduler: no se encontró ningún emulador de terminal instalado")
}
