// Package scheduler despierta las sesiones autorizadas cuando llega la hora de
// un encargo. Corre como proceso propio y no carga ninguna sesión: por eso
// puede quedarse vivo gastando casi nada mientras todo lo demás está cerrado.
package scheduler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/claudesessions"
	"reasonix/internal/schedulable"
)

// PermissionMode es el modo con el que corre todo encargo. Va fijo a propósito:
// un turno desatendido que se detiene a pedir aprobación se queda colgado para
// siempre, así que la autorización se decide al marcar la sesión, no a media
// madrugada.
const PermissionMode = "bypassPermissions"

// pollInterval es cada cuánto se revisa la cola. No necesita ser fino: los
// encargos se piensan en horas, no en segundos.
const pollInterval = 30 * time.Second

// Runner ejecuta los encargos vencidos. Binary es el ejecutable de Reasonix que
// se relanza; vacío significa el propio.
type Runner struct {
	Registry *schedulable.Registry
	Binary   string
	// ClaudeBinary reanuda las sesiones de Claude Code; vacío usa "claude".
	ClaudeBinary string
	// Now permite fijar el reloj en pruebas.
	Now func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// ClaudeBinary es el ejecutable que reanuda sesiones de Claude Code.
var ClaudeBinary = "claude"

func (r *Runner) binaryFor(entry schedulable.Entry) (string, error) {
	if entry.Agent() == schedulable.KindClaude {
		if strings.TrimSpace(r.ClaudeBinary) != "" {
			return r.ClaudeBinary, nil
		}
		return ClaudeBinary, nil
	}
	return r.binary()
}

func (r *Runner) binary() (string, error) {
	if strings.TrimSpace(r.Binary) != "" {
		return r.Binary, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("scheduler: no se pudo ubicar el ejecutable: %w", err)
	}
	return exe, nil
}

// ClaudePermissionFlag es el equivalente de PermissionMode en Claude Code.
const ClaudePermissionFlag = "--dangerously-skip-permissions"

// commandArgs arma la invocación que reanuda la sesión con el encargo. El
// prompt va al final porque es posicional en ambos agentes.
func commandArgs(entry schedulable.Entry, task schedulable.Task) []string {
	var args []string
	if entry.Agent() == schedulable.KindClaude {
		// Claude Code identifica la sesión por id, no por ruta, y el home donde
		// vive viaja aparte en el entorno.
		args = []string{"--resume", entry.SessionID, ClaudePermissionFlag}
	} else {
		args = []string{"--resume", entry.SessionPath, "--permission-mode", PermissionMode}
		if dir := strings.TrimSpace(entry.WorkDir); dir != "" {
			args = append(args, "--dir", dir)
		}
	}
	if model := strings.TrimSpace(task.Model); model != "" {
		args = append(args, "--model", model)
	}
	return append(args, "-p", task.Prompt)
}

// commandEnv agrega el home de configuración cuando la sesión es de Claude
// Code. Sin esa variable el binario busca en el home principal y no la halla.
func commandEnv(entry schedulable.Entry) []string {
	if entry.Agent() != schedulable.KindClaude || !claudesessions.NeedsConfigHome(entry.ConfigHome) {
		return nil
	}
	return []string{claudesessions.ResumeEnv + "=" + entry.ConfigHome}
}

// RunDue ejecuta lo vencido y devuelve cuántos encargos corrió. Un fallo se
// registra en la tarea y no detiene a las demás: una sesión rota no puede
// dejar sin correr al resto de la cola.
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	due, err := r.Registry.Due(r.now())
	if err != nil {
		return 0, err
	}
	ran := 0
	for _, task := range due {
		entry, err := r.Registry.Lookup(task.SessionID)
		if err != nil {
			// La autorización se retiró después de programar: el encargo muere
			// con ella, que es justo lo que significa revocar.
			_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskCancelled, err)
			continue
		}
		if !schedulable.SessionFileExists(entry.SessionPath) {
			// El transcript se borró después de autorizar. Decirlo aquí evita que
			// el fallo salga como un "no session matches" del binario relanzado.
			_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskFailed,
				fmt.Errorf("la sesión ya no existe en disco: %s", entry.SessionPath))
			continue
		}
		if task.Windowed() {
			// El encargo se atiende en una terminal para poder seguir la
			// conversación ahí mismo; solo se lanza, no se espera a que cierre.
			if err := OpenInTerminalWithPrompt(entry, task); err != nil {
				_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskFailed, err)
				continue
			}
			_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskDone, nil)
			ran++
			continue
		}
		if err := r.runOne(ctx, entry, task); err != nil {
			_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskFailed, err)
			continue
		}
		_ = r.Registry.FinishTask(ctx, task.ID, schedulable.TaskDone, nil)
		ran++
	}
	return ran, nil
}

// runOne relanza Reasonix sobre la sesión. La salida se descarta: la respuesta
// queda escrita en la sesión, que es donde el usuario la va a leer.
func (r *Runner) runOne(ctx context.Context, entry schedulable.Entry, task schedulable.Task) error {
	bin, err := r.binaryFor(entry)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, commandArgs(entry, task)...)
	if dir := strings.TrimSpace(entry.WorkDir); dir != "" {
		cmd.Dir = dir
	}
	if extra := commandEnv(entry); len(extra) > 0 {
		cmd.Env = append(os.Environ(), extra...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scheduler: la corrida falló: %w: %s", err, lastLine(string(out)))
	}
	return nil
}

// lastLine deja el final de la salida, que es donde suele estar el motivo real
// del fallo, acotado para que quepa en el registro de la tarea.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 300 {
		return last[:300]
	}
	return last
}

// Loop revisa la cola hasta que se cancele el contexto.
func (r *Runner) Loop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if _, err := r.RunDue(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "scheduler:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
