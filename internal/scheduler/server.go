package scheduler

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/agentmodels"
	"reasonix/internal/schedulable"
)

//go:embed panel.html
var panelHTML []byte

// guardHeader lo manda el panel en toda mutación. Un formulario de otra página
// no puede añadir cabeceras sin preflight, así que basta para que un sitio
// cualquiera no programe encargos contra el servidor local.
const guardHeader = "X-Reasonix-Panel"

// Server es el panel local: lista lo autorizado, programa encargos y cancela.
// No carga ninguna sesión, solo lee y escribe el registro en disco.
type Server struct {
	reg    *schedulable.Registry
	runner *Runner
	claude *claudeIndex
	// Rutas de configuración de donde salen los modelos de cada agente.
	reasonixConfig string
	claudeConfig   string
}

// LauncherSource agrupa de dónde salen los nombres de entorno: los .desktop
// del usuario y cuál de los homes es el principal.
type LauncherSource struct {
	Dir      string
	MainHome string
}

func NewServer(reg *schedulable.Registry, runner *Runner, claudeHomes []string,
	reasonixConfig, claudeConfig string, launchers LauncherSource) *Server {
	return &Server{
		reg: reg, runner: runner,
		claude:         newClaudeIndex(claudeHomes, launchers.Dir, launchers.MainHome),
		reasonixConfig: reasonixConfig, claudeConfig: claudeConfig,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.panel)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.cancelTask)
	mux.HandleFunc("POST /api/tasks/{id}/run", s.runTaskNow)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /api/tasks/clear", s.clearTasks)
	mux.HandleFunc("POST /api/sessions/{id}/revoke", s.revokeSession)
	mux.HandleFunc("GET /api/claude/sessions", s.claudeSessions)
	mux.HandleFunc("POST /api/claude/attach", s.claudeAttach)
	mux.HandleFunc("POST /api/claude/open", s.claudeOpen)
	mux.HandleFunc("POST /api/claude/refresh", s.claudeRefresh)
	mux.HandleFunc("GET /api/models", s.models)
	mux.HandleFunc("POST /api/sessions/{id}/open", s.openSession)
	return mux
}

func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(panelHTML)
}

// sessionView agrega si el transcript sigue en disco. Una sesión sin turnos se
// borra al cerrarse, y el panel tiene que mostrarla como rota en vez de dejar
// que el usuario le programe encargos que van a fallar.
type sessionView struct {
	schedulable.Entry
	Alive bool `json:"alive"`
}

type stateResponse struct {
	Sessions []sessionView      `json:"sessions"`
	Tasks    []schedulable.Task `json:"tasks"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.reg.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tasks, err := s.reg.Tasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]sessionView, 0, len(sessions))
	for _, e := range sessions {
		views = append(views, sessionView{Entry: e, Alive: schedulable.SessionFileExists(e.SessionPath)})
	}
	writeJSON(w, stateResponse{Sessions: views, Tasks: tasks})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		RunAt     string `json:"run_at"`
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		Mode      string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(body.RunAt))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("hora inválida: %w", err))
		return
	}
	mode := schedulable.ModeHeadless
	if strings.TrimSpace(body.Mode) == string(schedulable.ModeWindow) {
		mode = schedulable.ModeWindow
	}
	task, err := s.reg.Schedule(r.Context(), schedulable.Task{
		SessionID: body.SessionID,
		RunAt:     runAt,
		Prompt:    body.Prompt,
		Model:     body.Model,
		Mode:      mode,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, schedulable.ErrSessionNotSchedulable) {
			status = http.StatusForbidden
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, task)
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	if err := s.reg.CancelTask(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"status": "cancelled"})
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	if err := s.reg.DeleteTask(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// runTaskNow adelanta un encargo: lo pone vencido y deja que el runner lo tome,
// para que ejecutar a mano siga el mismo camino que la corrida automática.
func (s *Server) runTaskNow(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	tasks, err := s.reg.Tasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	id := r.PathValue("id")
	for _, t := range tasks {
		if t.ID != id {
			continue
		}
		t.RunAt = time.Now().UTC().Add(-time.Second)
		t.State = schedulable.TaskPending
		if _, err := s.reg.Schedule(r.Context(), t); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_, _ = s.runner.RunDue(ctx)
		}()
		writeJSON(w, map[string]string{"status": "running"})
		return
	}
	writeErr(w, http.StatusNotFound, fmt.Errorf("no existe la tarea %q", id))
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	if err := s.reg.Unmark(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"status": "revoked"})
}

func guarded(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(guardHeader) == "" {
		writeErr(w, http.StatusForbidden, errors.New("petición sin la cabecera del panel"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// models entrega los modelos de cada agente para que el selector no dependa de
// recordar cómo se escribe cada nombre.
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	reasonix, _ := agentmodels.Reasonix(s.reasonixConfig)
	claude, _ := agentmodels.Claude(s.claudeConfig)
	writeJSON(w, map[string]any{"reasonix": reasonix, "claude": claude})
}

// openSession abre una sesión ya autorizada en una terminal nueva.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	entry, err := s.reg.Lookup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err := OpenInTerminal(entry); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"status": "opened"})
}

// clearTasks vacía la lista de encargos ya terminados.
func (s *Server) clearTasks(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	removed, err := s.reg.ClearFinished(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]int{"removed": removed})
}
