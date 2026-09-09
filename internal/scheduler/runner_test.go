package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/schedulable"
)

// sessionFile fabrica un transcript real, porque autorizar exige que exista.
func sessionFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("crear transcript: %v", err)
	}
	return path
}

func testRegistry(t *testing.T) *schedulable.Registry {
	t.Helper()
	r, err := schedulable.Open(filepath.Join(t.TempDir(), "schedulable"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

func TestCommandArgsAlwaysRunsWithMaximumPermissions(t *testing.T) {
	// Un encargo corre sin nadie mirando: si se detuviera a pedir aprobación se
	// quedaría colgado hasta que alguien abra la sesión, o sea nunca.
	t.Parallel()
	args := commandArgs(
		schedulable.Entry{SessionPath: "/s/a.jsonl", WorkDir: "/home/u/Seo"},
		schedulable.Task{Prompt: "revisa el monitoreo"},
	)
	i := slices.Index(args, "--permission-mode")
	if i < 0 || i+1 >= len(args) || args[i+1] != PermissionMode {
		t.Fatalf("no se fijó el modo de permisos: %v", args)
	}
}

func TestCommandArgsCarriesSessionWorkdirAndPrompt(t *testing.T) {
	// La sesión se reanuda desde su propia carpeta: el planificador no hereda el
	// directorio de nadie, y el prompt va al final porque es posicional.
	t.Parallel()
	args := commandArgs(
		schedulable.Entry{SessionPath: "/s/a.jsonl", WorkDir: "/home/u/Seo"},
		schedulable.Task{Prompt: "revisa el monitoreo"},
	)
	if i := slices.Index(args, "--resume"); i < 0 || args[i+1] != "/s/a.jsonl" {
		t.Fatalf("falta la sesión: %v", args)
	}
	if i := slices.Index(args, "--dir"); i < 0 || args[i+1] != "/home/u/Seo" {
		t.Fatalf("falta el directorio: %v", args)
	}
	if args[len(args)-1] != "revisa el monitoreo" {
		t.Fatalf("el prompt no quedó al final: %v", args)
	}
}

func TestCommandArgsOmitsModelWhenNotChosen(t *testing.T) {
	// Sin modelo elegido no se pasa la bandera, para que la sesión siga con el
	// modelo con el que se guardó en vez de que el planificador decida.
	t.Parallel()
	args := commandArgs(schedulable.Entry{SessionPath: "/s/a.jsonl"}, schedulable.Task{Prompt: "hola"})
	if slices.Contains(args, "--model") {
		t.Fatalf("se coló --model sin haberlo pedido: %v", args)
	}
	withModel := commandArgs(schedulable.Entry{SessionPath: "/s/a.jsonl"},
		schedulable.Task{Prompt: "hola", Model: "deepseek-chat"})
	if i := slices.Index(withModel, "--model"); i < 0 || withModel[i+1] != "deepseek-chat" {
		t.Fatalf("no se pasó el modelo elegido: %v", withModel)
	}
}

func TestRunDueCancelsTasksWhoseAuthorizationWasRevoked(t *testing.T) {
	// Si el usuario quita la marca después de programar, el encargo no debe
	// correr: revocar tiene que alcanzar también a lo ya agendado.
	t.Parallel()
	reg := testRegistry(t)
	ctx := context.Background()
	if _, err := reg.Mark(ctx, schedulable.Entry{SessionID: "s1", SessionPath: sessionFile(t, "s1.jsonl")}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if _, err := reg.Schedule(ctx, schedulable.Task{
		SessionID: "s1", RunAt: time.Now().Add(-time.Minute), Prompt: "revisa",
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := reg.Unmark(ctx, "s1"); err != nil {
		t.Fatalf("Unmark: %v", err)
	}
	runner := &Runner{Registry: reg, Binary: "/bin/false"}
	ran, err := runner.RunDue(ctx)
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if ran != 0 {
		t.Fatalf("corrió %d encargos de una sesión sin autorización", ran)
	}
	tasks, err := reg.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].State != schedulable.TaskCancelled {
		t.Fatalf("la tarea no quedó cancelada: %+v", tasks)
	}
}

func TestRunDueRecordsFailureWhenTheBinaryFails(t *testing.T) {
	// Si la corrida truena, la tarea queda marcada como fallida con el motivo,
	// para que el panel lo pueda mostrar en vez de dejarla pendiente para siempre.
	if runtime.GOOS == "windows" {
		t.Skip("usa /bin/false")
	}
	t.Parallel()
	reg := testRegistry(t)
	ctx := context.Background()
	if _, err := reg.Mark(ctx, schedulable.Entry{
		SessionID: "s1", SessionPath: sessionFile(t, "s1.jsonl"), WorkDir: os.TempDir(),
	}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if _, err := reg.Schedule(ctx, schedulable.Task{
		SessionID: "s1", RunAt: time.Now().Add(-time.Minute), Prompt: "revisa",
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	runner := &Runner{Registry: reg, Binary: "/bin/false"}
	if _, err := runner.RunDue(ctx); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	tasks, err := reg.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].State != schedulable.TaskFailed || tasks[0].LastError == "" {
		t.Fatalf("no se registró el fallo: %+v", tasks)
	}
}

func TestRunDueFailsClearlyWhenTheSessionFileIsGone(t *testing.T) {
	// Si el transcript desapareció después de autorizar, el fallo tiene que
	// nombrarlo. Sin esto el error que llega es un "no session matches" del
	// binario relanzado, que no dice qué hacer al respecto.
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("crear transcript: %v", err)
	}
	reg := testRegistry(t)
	ctx := context.Background()
	if _, err := reg.Mark(ctx, schedulable.Entry{SessionID: "s1", SessionPath: path}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if _, err := reg.Schedule(ctx, schedulable.Task{
		SessionID: "s1", RunAt: time.Now().Add(-time.Minute), Prompt: "revisa",
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("borrar transcript: %v", err)
	}
	runner := &Runner{Registry: reg, Binary: "/bin/false"}
	if _, err := runner.RunDue(ctx); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	tasks, err := reg.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].State != schedulable.TaskFailed ||
		!strings.Contains(tasks[0].LastError, "ya no existe en disco") {
		t.Fatalf("el fallo no nombra la sesión perdida: %+v", tasks)
	}
}

func TestCommandArgsForClaudeUsesSessionIDAndItsOwnPermissionFlag(t *testing.T) {
	// Claude Code identifica la sesión por id, no por ruta, y su bandera de
	// permisos se llama distinto. Pasarle los de Reasonix no reanuda nada.
	t.Parallel()
	args := commandArgs(
		schedulable.Entry{
			Kind:        schedulable.KindClaude,
			SessionID:   "6b771ab4-0bcf",
			SessionPath: "/home/u/.claude/projects/x/6b771ab4-0bcf.jsonl",
			ConfigHome:  "/home/u/.claude-assistant",
		},
		schedulable.Task{Prompt: "revisa el monitoreo"},
	)
	if i := slices.Index(args, "--resume"); i < 0 || args[i+1] != "6b771ab4-0bcf" {
		t.Fatalf("no reanuda por id: %v", args)
	}
	if !slices.Contains(args, ClaudePermissionFlag) {
		t.Fatalf("falta la bandera de permisos de Claude: %v", args)
	}
	if slices.Contains(args, "--permission-mode") {
		t.Fatalf("se coló la bandera de Reasonix: %v", args)
	}
}

func TestCommandEnvCarriesTheClaudeConfigHome(t *testing.T) {
	// Una sesión del home del asistente no existe para el home principal: sin
	// CLAUDE_CONFIG_DIR el binario no la encuentra y el encargo se pierde.
	t.Parallel()
	env := commandEnv(schedulable.Entry{
		Kind:       schedulable.KindClaude,
		ConfigHome: "/home/u/.claude-assistant",
	})
	if len(env) != 1 || !strings.Contains(env[0], "/home/u/.claude-assistant") {
		t.Fatalf("no viaja el home de configuración: %v", env)
	}
	if got := commandEnv(schedulable.Entry{SessionPath: "/s/a.jsonl"}); got != nil {
		t.Fatalf("una sesión de Reasonix no debe llevar ese entorno: %v", got)
	}
}
