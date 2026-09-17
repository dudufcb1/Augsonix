package scheduler

import (
	"strings"
	"testing"

	"reasonix/internal/claudesessions"
	"reasonix/internal/schedulable"
)

func TestResumeShellCommandForClaudeCarriesItsConfigHome(t *testing.T) {
	// Abrir una sesión del home del asistente sin esa variable arrancaría una
	// sesión nueva en el home principal en vez de retomar la que se pidió.
	t.Parallel()
	cmd := resumeShellCommand(schedulable.Entry{
		Kind:       schedulable.KindClaude,
		SessionID:  "abc-123",
		ConfigHome: "/home/u/.claude-assistant",
	})
	if !strings.Contains(cmd, "CLAUDE_CONFIG_DIR='/home/u/.claude-assistant'") {
		t.Fatalf("falta el home de configuración: %s", cmd)
	}
	if !strings.Contains(cmd, "claude --resume 'abc-123'") {
		t.Fatalf("no reanuda por id: %s", cmd)
	}
}

func TestResumeShellCommandForReasonixUsesThePath(t *testing.T) {
	// Reasonix reanuda por ruta de archivo, y no lleva la variable de Claude.
	t.Parallel()
	cmd := resumeShellCommand(schedulable.Entry{SessionPath: "/s/a.jsonl"})
	if !strings.Contains(cmd, "reasonix --resume '/s/a.jsonl'") {
		t.Fatalf("comando inesperado: %s", cmd)
	}
	if strings.Contains(cmd, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("se coló la variable de Claude: %s", cmd)
	}
}

func TestShellQuoteNeutralizesQuotesInPaths(t *testing.T) {
	// Una carpeta con apóstrofo cerraría la cadena y el resto se ejecutaría como
	// comandos sueltos.
	t.Parallel()
	got := shellQuote("/home/u/Mi'carpeta")
	if strings.Contains(got, "Mi'carpeta") {
		t.Fatalf("la comilla quedó sin escapar: %s", got)
	}
}

func TestResumeShellCommandOmitsConfigHomeForTheMainHome(t *testing.T) {
	// Pasarle CLAUDE_CONFIG_DIR apuntando al home principal hace que Claude Code
	// ignore su configuración y arranque en la pantalla de bienvenida en vez de
	// reanudar. La variable solo tiene sentido para los homes alternos.
	t.Parallel()
	cmd := resumeShellCommand(schedulable.Entry{
		Kind:       schedulable.KindClaude,
		SessionID:  "abc-123",
		ConfigHome: claudesessions.DefaultHome(),
	})
	if strings.Contains(cmd, claudesessions.ResumeEnv) {
		t.Fatalf("se exportó el home principal: %s", cmd)
	}
	if !strings.Contains(cmd, "claude --resume 'abc-123'") {
		t.Fatalf("no reanuda la sesión: %s", cmd)
	}
}

func TestResumeShellCommandWithPromptCarriesTheTaskInline(t *testing.T) {
	// En modo ventana el encargo va como primer mensaje, para llegar a una
	// conversación ya empezada y no a un cursor vacío.
	t.Parallel()
	cmd := resumeShellCommandWith(
		schedulable.Entry{Kind: schedulable.KindClaude, SessionID: "abc", ConfigHome: "/home/u/.claude-assistant"},
		schedulable.Task{Prompt: "revisa el monitoreo", Model: "sonnet"},
	)
	if !strings.Contains(cmd, "'revisa el monitoreo'") {
		t.Fatalf("no lleva el encargo: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'sonnet'") {
		t.Fatalf("no lleva el modelo: %s", cmd)
	}
	if strings.Contains(cmd, "-p ") {
		t.Fatalf("modo ventana no debe correr headless: %s", cmd)
	}
	if !strings.HasSuffix(cmd, "; exec bash") {
		t.Fatalf("la ventana se cerraría al terminar: %s", cmd)
	}
}

func TestStartTerminalReportsImmediateFailureWithStderr(t *testing.T) {
	// Si el emulador muere al instante (sin DISPLAY, gnome-terminal imprime "no
	// se puede abrir el visor" y sale con 1), el error debe traer ese texto en
	// vez de reportar la ventana como abierta.
	t.Parallel()
	err := startTerminal("fake-term", "/bin/sh", []string{"-c", "echo 'no se puede abrir el visor' >&2; exit 1"})
	if err == nil {
		t.Fatal("un emulador que sale con error se reportó como abierto")
	}
	if !strings.Contains(err.Error(), "no se puede abrir el visor") {
		t.Fatalf("el error no trae el stderr del emulador: %v", err)
	}
}

func TestStartTerminalTreatsQuickCleanExitAsOpened(t *testing.T) {
	// gnome-terminal delega la ventana a su servidor y termina con 0 en
	// milisegundos; eso es éxito, no fallo.
	t.Parallel()
	if err := startTerminal("fake-term", "/bin/sh", []string{"-c", "exit 0"}); err != nil {
		t.Fatalf("una salida limpia se tomó como error: %v", err)
	}
}

func TestOpenTerminalFailsWhenTheAgentIsNotOnPath(t *testing.T) {
	// La terminal hereda el PATH del servicio; si ahí no está `claude`, la
	// ventana abriría con "command not found" sin que el panel se entere. Se
	// detecta antes de lanzar nada.
	t.Setenv("PATH", t.TempDir())
	err := openTerminal(schedulable.Entry{Kind: schedulable.KindClaude, SessionID: "abc"}, "claude --resume abc")
	if err == nil || !strings.Contains(err.Error(), `"claude"`) {
		t.Fatalf("no se detectó el binario ausente: %v", err)
	}
}
