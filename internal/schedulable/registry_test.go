package schedulable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func newRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "schedulable"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

func TestLookupFailsForUnmarkedSession(t *testing.T) {
	// Una sesión guardada no es despertable por el solo hecho de existir: si el
	// usuario no la marcó, el planificador tiene que rebotar.
	t.Parallel()
	r := newRegistry(t)
	if _, err := r.Lookup("sesion-cualquiera"); err != ErrNotMarked {
		t.Fatalf("Lookup = %v, se esperaba ErrNotMarked", err)
	}
}

func TestMarkThenLookupAuthorizesTheSession(t *testing.T) {
	// Tras marcarla, el planificador la encuentra con los datos que necesita
	// para reanudarla: la ruta del transcript y desde dónde correr.
	t.Parallel()
	r := newRegistry(t)
	if _, err := r.Mark(context.Background(), Entry{
		SessionID:   "abc123",
		SessionPath: sessionFile(t, "abc123.jsonl"),
		WorkDir:     "/home/u/Seo",
		Title:       "monitoreo",
	}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	got, err := r.Lookup("abc123")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.WorkDir != "/home/u/Seo" || got.SessionPath == "" {
		t.Fatalf("entrada incompleta: %+v", got)
	}
	if got.MarkedAt.IsZero() || got.SchemaVersion != SchemaVersion {
		t.Fatalf("falta sello de marcado o versión: %+v", got)
	}
}

func TestMarkTwiceKeepsTheOriginalDate(t *testing.T) {
	// Volver a marcar no debe reiniciar la fecha: el registro tiene que poder
	// decir desde cuándo está autorizada, no cuándo se tocó por última vez.
	t.Parallel()
	r := newRegistry(t)
	ctx := context.Background()
	first, err := r.Mark(ctx, Entry{SessionID: "abc", SessionPath: sessionFile(t, "abc.jsonl")})
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	second, err := r.Mark(ctx, Entry{SessionID: "abc", SessionPath: sessionFile(t, "abc.jsonl"), Title: "otro"})
	if err != nil {
		t.Fatalf("Mark otra vez: %v", err)
	}
	if !second.MarkedAt.Equal(first.MarkedAt) {
		t.Fatalf("la fecha cambió: %v -> %v", first.MarkedAt, second.MarkedAt)
	}
}

func TestUnmarkRevokesTheAuthorization(t *testing.T) {
	// Quitar la marca es la forma en que el usuario retira el permiso, y una
	// sesión ya retirada se puede volver a quitar sin que eso sea un error.
	t.Parallel()
	r := newRegistry(t)
	ctx := context.Background()
	if _, err := r.Mark(ctx, Entry{SessionID: "abc", SessionPath: sessionFile(t, "abc.jsonl")}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if err := r.Unmark(ctx, "abc"); err != nil {
		t.Fatalf("Unmark: %v", err)
	}
	if _, err := r.Lookup("abc"); err != ErrNotMarked {
		t.Fatalf("sigue autorizada tras Unmark: %v", err)
	}
	if err := r.Unmark(ctx, "abc"); err != nil {
		t.Fatalf("Unmark repetido devolvió error: %v", err)
	}
}

func TestListOrdersNewestFirst(t *testing.T) {
	// El panel muestra primero lo marcado más recientemente, que es lo que el
	// usuario acaba de dejar encargado.
	t.Parallel()
	r := newRegistry(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := r.Mark(ctx, Entry{SessionID: "vieja", SessionPath: sessionFile(t, "v.jsonl"), MarkedAt: old}); err != nil {
		t.Fatalf("Mark vieja: %v", err)
	}
	if _, err := r.Mark(ctx, Entry{SessionID: "nueva", SessionPath: sessionFile(t, "n.jsonl")}); err != nil {
		t.Fatalf("Mark nueva: %v", err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0].SessionID != "nueva" {
		t.Fatalf("orden inesperado: %+v", list)
	}
}

func TestMarkRejectsSessionIDThatEscapesTheDirectory(t *testing.T) {
	// Un id con separadores o con ".." escribiría la autorización fuera del
	// registro, que es justo el archivo que decide a quién se puede despertar.
	t.Parallel()
	r := newRegistry(t)
	ctx := context.Background()
	for _, id := range []string{"../fuera", "sub/dir", "..", ""} {
		if _, err := r.Mark(ctx, Entry{SessionID: id, SessionPath: sessionFile(t, "x.jsonl")}); err == nil {
			t.Fatalf("se aceptó un session id inválido: %q", id)
		}
	}
}

func TestListIgnoresUnreadableEntries(t *testing.T) {
	// Una entrada corrupta no puede tumbar el listado: el panel tiene que poder
	// seguir mostrando las demás sesiones autorizadas.
	t.Parallel()
	r := newRegistry(t)
	if _, err := r.Mark(context.Background(), Entry{SessionID: "buena", SessionPath: sessionFile(t, "b.jsonl")}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if err := writeRaw(filepath.Join(r.dir, "rota.json"), "{esto no es json"); err != nil {
		t.Fatalf("preparar entrada corrupta: %v", err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].SessionID != "buena" {
		t.Fatalf("la entrada corrupta afectó el listado: %+v", list)
	}
}

// writeRaw deja contenido tal cual en disco, para fabricar entradas inválidas.
func writeRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestMarkRejectsASessionWithNoFileOnDisk(t *testing.T) {
	// Una sesión sin turnos guardados se borra al cerrarse. Autorizarla dejaría
	// un permiso apuntando a un archivo fantasma que el planificador no podría
	// reanudar, y el error saldría hasta la madrugada.
	t.Parallel()
	r := newRegistry(t)
	_, err := r.Mark(context.Background(), Entry{
		SessionID:   "fantasma",
		SessionPath: filepath.Join(t.TempDir(), "no-existe.jsonl"),
	})
	if !errors.Is(err, ErrSessionFileMissing) {
		t.Fatalf("Mark = %v, se esperaba ErrSessionFileMissing", err)
	}
}
