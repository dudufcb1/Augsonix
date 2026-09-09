// Package schedulable es la lista blanca de sesiones que el planificador puede
// despertar. Marcar es una decisión explícita del usuario y no caduca sola: una
// sesión guardada nunca es despertable por el solo hecho de existir.
//
// El registro vive aparte del catálogo de sesiones para que listar lo agendable
// no obligue a recorrer miles de transcripts, y para que desmarcar sea borrar
// una entrada en vez de reescribir la conversación.
package schedulable

import (
	"context"
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

// SchemaVersion identifica el formato de las entradas en disco.
const SchemaVersion = 1

// ErrNotMarked indica que la sesión no está en la lista blanca.
var ErrNotMarked = errors.New("schedulable: la sesión no está marcada como agendable")

// ErrSessionFileMissing señala una sesión cuyo transcript no está en disco. Una
// sesión sin turnos guardados se borra al cerrarse, así que autorizarla dejaría
// un permiso apuntando a un archivo que nadie va a poder reanudar.
var ErrSessionFileMissing = errors.New("schedulable: el archivo de la sesión no existe")

// Kind distingue qué agente reanuda la sesión. El vacío es Reasonix, para que
// las entradas escritas antes de que existiera Claude Code sigan valiendo.
type Kind string

const (
	KindReasonix Kind = "reasonix"
	KindClaude   Kind = "claude"
)

// Entry es una sesión que el usuario autorizó a despertar. WorkDir se guarda
// porque el planificador reanuda sin heredar el directorio de nadie. ConfigHome
// solo aplica a Claude Code, donde la sesión únicamente existe dentro del home
// de configuración en que se creó.
type Entry struct {
	SchemaVersion int       `json:"schema_version"`
	Kind          Kind      `json:"kind,omitempty"`
	SessionID     string    `json:"session_id"`
	SessionPath   string    `json:"session_path"`
	WorkDir       string    `json:"work_dir"`
	ConfigHome    string    `json:"config_home,omitempty"`
	Title         string    `json:"title,omitempty"`
	MarkedAt      time.Time `json:"marked_at"`
}

// Agent devuelve el tipo efectivo, tratando el vacío como Reasonix.
func (e Entry) Agent() Kind {
	if e.Kind == KindClaude {
		return KindClaude
	}
	return KindReasonix
}

// Registry guarda una entrada por archivo, de modo que dos procesos marcando
// sesiones distintas nunca se pisan.
type Registry struct{ dir string }

// Open deja el directorio listo para escribir.
func Open(dir string) (*Registry, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("schedulable: directorio vacío")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("schedulable: crear %s: %w", dir, err)
	}
	return &Registry{dir: dir}, nil
}

// entryPath valida el identificador antes de convertirlo en ruta: un id con
// separadores o con ".." escribiría fuera del registro.
func (r *Registry) entryPath(sessionID string) (string, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", fmt.Errorf("schedulable: session id vacío")
	}
	if id != filepath.Base(id) || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("schedulable: session id inválido: %q", sessionID)
	}
	return filepath.Join(r.dir, id+".json"), nil
}

// Mark autoriza a despertar la sesión. Repetirlo sobre una ya marcada conserva
// la fecha original, para que el registro diga desde cuándo está autorizada.
func (r *Registry) Mark(ctx context.Context, e Entry) (Entry, error) {
	path, err := r.entryPath(e.SessionID)
	if err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(e.SessionPath) == "" {
		return Entry{}, fmt.Errorf("schedulable: falta la ruta de la sesión")
	}
	if !SessionFileExists(e.SessionPath) {
		return Entry{}, fmt.Errorf("%w: %s", ErrSessionFileMissing, e.SessionPath)
	}
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return Entry{}, fmt.Errorf("schedulable: tomar el candado: %w", err)
	}
	defer release()

	e.SchemaVersion = SchemaVersion
	if prev, err := readEntry(path); err == nil && !prev.MarkedAt.IsZero() {
		e.MarkedAt = prev.MarkedAt
	} else if e.MarkedAt.IsZero() {
		e.MarkedAt = time.Now().UTC()
	}
	if err := writeEntry(path, e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Unmark retira la autorización. Quitar algo que no estaba no es un error: el
// resultado que importa es que la sesión ya no se puede despertar.
func (r *Registry) Unmark(ctx context.Context, sessionID string) error {
	path, err := r.entryPath(sessionID)
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

// Lookup responde si el planificador puede tocar esa sesión.
func (r *Registry) Lookup(sessionID string) (Entry, error) {
	path, err := r.entryPath(sessionID)
	if err != nil {
		return Entry{}, err
	}
	e, err := readEntry(path)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, ErrNotMarked
	}
	return e, err
}

// List devuelve lo agendable, de lo marcado más recientemente a lo más viejo.
// Una entrada ilegible se omite en vez de tumbar el listado: el panel tiene que
// poder mostrar las demás.
func (r *Registry) List() ([]Entry, error) {
	names, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("schedulable: leer %s: %w", r.dir, err)
	}
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		if n.IsDir() || !strings.HasSuffix(n.Name(), ".json") {
			continue
		}
		e, err := readEntry(filepath.Join(r.dir, n.Name()))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MarkedAt.After(out[j].MarkedAt) })
	return out, nil
}

func readEntry(path string) (Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, fmt.Errorf("schedulable: %s ilegible: %w", path, err)
	}
	return e, nil
}

// writeEntry escribe por temporal y rename, para que una interrupción no deje
// una entrada a medias que el planificador leería como autorización válida.
func writeEntry(path string, e Entry) error {
	raw, err := json.MarshalIndent(e, "", "  ")
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

// SessionFileExists dice si el transcript sigue en disco. El planificador lo
// consulta antes de reanudar: una sesión borrada no se puede despertar, y el
// error tiene que decir eso en vez de morir dentro del binario relanzado.
func SessionFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
