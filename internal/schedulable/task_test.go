package schedulable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// markedRegistry entrega un registro con una sesión ya autorizada.
func markedRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	r := newRegistry(t)
	const id = "sesion1"
	if _, err := r.Mark(context.Background(), Entry{SessionID: id, SessionPath: sessionFile(t, "sesion1.jsonl")}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	return r, id
}

func TestScheduleRejectsSessionThatIsNotSchedulable(t *testing.T) {
	// La lista blanca es la única puerta: sin marcar la sesión, programarle un
	// encargo tiene que fallar aunque el resto de los datos sean válidos.
	t.Parallel()
	r := newRegistry(t)
	_, err := r.Schedule(context.Background(), Task{
		SessionID: "no-autorizada",
		RunAt:     time.Now().Add(time.Hour),
		Prompt:    "revisa el monitoreo",
	})
	if !errors.Is(err, ErrSessionNotSchedulable) {
		t.Fatalf("Schedule = %v, se esperaba ErrSessionNotSchedulable", err)
	}
}

func TestScheduleRejectsEmptyPrompt(t *testing.T) {
	// Un encargo sin mensaje despertaría la sesión sin nada que pedirle.
	t.Parallel()
	r, id := markedRegistry(t)
	if _, err := r.Schedule(context.Background(), Task{
		SessionID: id,
		RunAt:     time.Now().Add(time.Hour),
		Prompt:    "   ",
	}); err == nil {
		t.Fatal("se aceptó un encargo con mensaje vacío")
	}
}

func TestDueReturnsOnlyTasksWhoseTimeHasPassed(t *testing.T) {
	// El planificador solo debe levantar lo vencido: una tarea para más tarde
	// no puede colarse en la corrida de ahorita.
	t.Parallel()
	r, id := markedRegistry(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(-time.Minute), Prompt: "ya"}); err != nil {
		t.Fatalf("Schedule vencida: %v", err)
	}
	if _, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(time.Hour), Prompt: "luego"}); err != nil {
		t.Fatalf("Schedule futura: %v", err)
	}
	due, err := r.Due(now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 || due[0].Prompt != "ya" {
		t.Fatalf("Due devolvió %+v", due)
	}
}

func TestFinishTaskStopsItFromRunningAgain(t *testing.T) {
	// Cerrar el encargo evita que un reinicio del planificador lo repita: una
	// tarea ya corrida no puede volver a despertar la sesión.
	t.Parallel()
	r, id := markedRegistry(t)
	ctx := context.Background()
	now := time.Now().UTC()
	task, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(-time.Minute), Prompt: "revisa"})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := r.FinishTask(ctx, task.ID, TaskDone, nil); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	due, err := r.Due(now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("la tarea terminada volvió a salir: %+v", due)
	}
}

func TestFinishTaskRecordsTheFailure(t *testing.T) {
	// Si la corrida falla hay que poder verlo en el panel, no solo saber que ya
	// no está pendiente.
	t.Parallel()
	r, id := markedRegistry(t)
	ctx := context.Background()
	task, err := r.Schedule(ctx, Task{SessionID: id, RunAt: time.Now().Add(-time.Minute), Prompt: "revisa"})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := r.FinishTask(ctx, task.ID, TaskFailed, errors.New("el binario no arrancó")); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	all, err := r.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(all) != 1 || all[0].State != TaskFailed || all[0].LastError == "" {
		t.Fatalf("no se registró el fallo: %+v", all)
	}
}

func TestCancelTaskRemovesItFromTheQueue(t *testing.T) {
	// Cancelar desde el panel deja rastro pero saca la tarea de la cola.
	t.Parallel()
	r, id := markedRegistry(t)
	ctx := context.Background()
	now := time.Now().UTC()
	task, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(-time.Minute), Prompt: "revisa"})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := r.CancelTask(ctx, task.ID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	due, err := r.Due(now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("la tarea cancelada sigue en la cola: %+v", due)
	}
}

func TestMarkedSessionListingIgnoresTheTasksDirectory(t *testing.T) {
	// Las tareas viven en una subcarpeta del registro; el listado de sesiones
	// autorizadas no debe confundirla con una entrada.
	t.Parallel()
	r, id := markedRegistry(t)
	if _, err := r.Schedule(context.Background(), Task{
		SessionID: id, RunAt: time.Now().Add(time.Hour), Prompt: "luego",
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].SessionID != id {
		t.Fatalf("listado contaminado: %+v", list)
	}
}

func TestClearFinishedKeepsWhatIsStillPending(t *testing.T) {
	// Limpiar la vista no puede llevarse lo que falta por correr: un encargo
	// pendiente es trabajo agendado, no historial.
	t.Parallel()
	r, id := markedRegistry(t)
	ctx := context.Background()
	now := time.Now().UTC()
	hecha, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(-time.Hour), Prompt: "ya corrió"})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if err := r.FinishTask(ctx, hecha.ID, TaskDone, nil); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	if _, err := r.Schedule(ctx, Task{SessionID: id, RunAt: now.Add(time.Hour), Prompt: "mañana"}); err != nil {
		t.Fatalf("Schedule pendiente: %v", err)
	}
	removed, err := r.ClearFinished(ctx)
	if err != nil {
		t.Fatalf("ClearFinished: %v", err)
	}
	if removed != 1 {
		t.Fatalf("quitó %d encargos, se esperaba 1", removed)
	}
	rest, err := r.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(rest) != 1 || rest[0].Prompt != "mañana" {
		t.Fatalf("se borró lo pendiente: %+v", rest)
	}
}
