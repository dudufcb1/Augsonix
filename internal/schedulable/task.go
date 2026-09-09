package schedulable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/filelock"
)

// TaskState es el ciclo de vida de un encargo programado.
type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskDone      TaskState = "done"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
)

// ErrSessionNotSchedulable rechaza programar sobre una sesión sin autorizar.
var ErrSessionNotSchedulable = errors.New("schedulable: la sesión no está marcada como agendable")

// TaskMode decide si el encargo corre sin ventana o abre una terminal. El vacío
// es headless, que es como se comportaban los encargos antes de existir esto.
type TaskMode string

const (
	ModeHeadless TaskMode = "headless"
	ModeWindow   TaskMode = "window"
)

// Windowed dice si el encargo debe abrir una terminal para seguir ahí.
func (t Task) Windowed() bool { return t.Mode == ModeWindow }

// Task es un encargo con hora. Model vacío significa el modelo con el que se
// guardó la sesión; el planificador no elige por su cuenta.
type Task struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	RunAt         time.Time `json:"run_at"`
	Prompt        string    `json:"prompt"`
	Model         string    `json:"model,omitempty"`
	Mode          TaskMode  `json:"mode,omitempty"`
	State         TaskState `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	RanAt         time.Time `json:"ran_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

func (r *Registry) tasksDir() string { return filepath.Join(r.dir, "tasks") }

func (r *Registry) taskPath(id string) (string, error) {
	clean := strings.TrimSpace(id)
	if clean == "" || clean != filepath.Base(clean) || strings.ContainsAny(clean, `/\`) {
		return "", fmt.Errorf("schedulable: id de tarea inválido: %q", id)
	}
	return filepath.Join(r.tasksDir(), clean+".json"), nil
}

// Schedule guarda un encargo. Exige que la sesión ya esté autorizada: la lista
// blanca es la única puerta, así una tarea nunca puede despertar algo que el
// usuario no marcó.
func (r *Registry) Schedule(ctx context.Context, t Task) (Task, error) {
	if _, err := r.Lookup(t.SessionID); err != nil {
		if errors.Is(err, ErrNotMarked) {
			return Task{}, ErrSessionNotSchedulable
		}
		return Task{}, err
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return Task{}, fmt.Errorf("schedulable: el encargo no puede ir vacío")
	}
	if t.RunAt.IsZero() {
		return Task{}, fmt.Errorf("schedulable: falta la hora de ejecución")
	}
	if err := os.MkdirAll(r.tasksDir(), 0o700); err != nil {
		return Task{}, fmt.Errorf("schedulable: crear %s: %w", r.tasksDir(), err)
	}
	if t.ID == "" {
		id, err := newTaskID()
		if err != nil {
			return Task{}, err
		}
		t.ID = id
	}
	path, err := r.taskPath(t.ID)
	if err != nil {
		return Task{}, err
	}
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return Task{}, fmt.Errorf("schedulable: tomar el candado: %w", err)
	}
	defer release()

	t.SchemaVersion = SchemaVersion
	if t.State == "" {
		t.State = TaskPending
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.RunAt = t.RunAt.UTC()
	if err := writeTask(path, t); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Due devuelve los encargos pendientes cuya hora ya pasó, del más viejo al más
// nuevo, para que un retraso no altere el orden en que se pensaron.
func (r *Registry) Due(now time.Time) ([]Task, error) {
	all, err := r.Tasks()
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(all))
	for _, t := range all {
		if t.State == TaskPending && !t.RunAt.After(now.UTC()) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunAt.Before(out[j].RunAt) })
	return out, nil
}

// Tasks lista todos los encargos, del más próximo al más lejano. Una tarea
// ilegible se omite en vez de tumbar el listado.
func (r *Registry) Tasks() ([]Task, error) {
	names, err := os.ReadDir(r.tasksDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("schedulable: leer %s: %w", r.tasksDir(), err)
	}
	out := make([]Task, 0, len(names))
	for _, n := range names {
		if n.IsDir() || !strings.HasSuffix(n.Name(), ".json") {
			continue
		}
		t, err := readTask(filepath.Join(r.tasksDir(), n.Name()))
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunAt.Before(out[j].RunAt) })
	return out, nil
}

// FinishTask cierra un encargo con su resultado. Escribe el estado final antes
// de que nadie lo vuelva a leer, para que un reinicio no lo repita.
func (r *Registry) FinishTask(ctx context.Context, id string, state TaskState, runErr error) error {
	path, err := r.taskPath(id)
	if err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("schedulable: tomar el candado: %w", err)
	}
	defer release()
	t, err := readTask(path)
	if err != nil {
		return err
	}
	t.State = state
	t.RanAt = time.Now().UTC()
	if runErr != nil {
		t.LastError = runErr.Error()
	}
	return writeTask(path, t)
}

// CancelTask retira un encargo pendiente sin borrar su rastro.
func (r *Registry) CancelTask(ctx context.Context, id string) error {
	return r.FinishTask(ctx, id, TaskCancelled, nil)
}

// DeleteTask borra el encargo del registro.
func (r *Registry) DeleteTask(ctx context.Context, id string) error {
	path, err := r.taskPath(id)
	if err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("schedulable: tomar el candado: %w", err)
	}
	defer release()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("schedulable: borrar %s: %w", path, err)
	}
	return nil
}

func newTaskID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("schedulable: generar id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func readTask(path string) (Task, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Task{}, err
	}
	var t Task
	if err := json.Unmarshal(raw, &t); err != nil {
		return Task{}, fmt.Errorf("schedulable: %s ilegible: %w", path, err)
	}
	return t, nil
}

func writeTask(path string, t Task) error {
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("schedulable: serializar: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("schedulable: escribir %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("schedulable: publicar %s: %w", path, err)
	}
	return nil
}

// ClearFinished borra los encargos que ya no van a correr y devuelve cuántos
// quitó. Los pendientes se conservan: limpiar la vista no puede ser también la
// forma de perder trabajo agendado.
func (r *Registry) ClearFinished(ctx context.Context) (int, error) {
	tasks, err := r.Tasks()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, t := range tasks {
		if t.State == TaskPending {
			continue
		}
		if err := r.DeleteTask(ctx, t.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
