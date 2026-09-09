package scheduler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/claudesessions"
	"reasonix/internal/schedulable"
)

// scanTTL evita repetir el barrido de cientos de transcripts en cada refresco
// del panel. Las sesiones nuevas tardan a lo sumo este rato en aparecer.
const scanTTL = 20 * time.Second

// claudeIndex cachea el último barrido de sesiones de Claude Code.
type claudeIndex struct {
	homes []string
	// launcherDir y mainHome sirven para nombrar el entorno de cada sesión con
	// el mismo nombre que el usuario le puso a su acceso de escritorio.
	launcherDir string
	mainHome    string

	mu       sync.Mutex
	cached   []claudesessions.Session
	scanned  time.Time
	scanning bool
}

func newClaudeIndex(homes []string, launcherDir, mainHome string) *claudeIndex {
	return &claudeIndex{homes: homes, launcherDir: launcherDir, mainHome: mainHome}
}

// sessions devuelve el listado, rebarriendo solo cuando el caché venció.
func (c *claudeIndex) sessions() ([]claudesessions.Session, error) {
	c.mu.Lock()
	fresh := time.Since(c.scanned) < scanTTL && c.cached != nil
	if fresh || c.scanning {
		defer c.mu.Unlock()
		return c.cached, nil
	}
	c.scanning = true
	c.mu.Unlock()

	list, err := claudesessions.List(c.homes, 0)
	if err == nil {
		list = claudesessions.ApplyLaunchers(list,
			claudesessions.LoadLaunchers(c.launcherDir, c.mainHome))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.scanning = false
	if err != nil {
		return c.cached, err
	}
	c.cached, c.scanned = list, time.Now()
	return list, nil
}

// invalidate fuerza un barrido nuevo en la próxima consulta.
func (c *claudeIndex) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scanned = time.Time{}
}

func (c *claudeIndex) find(id string) (claudesessions.Session, bool) {
	list, _ := c.sessions()
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return claudesessions.Session{}, false
}

// environmentOf es la etiqueta con la que el usuario reconoce la sesión: el
// nombre de su lanzador si lo hay, y si no el del home.
func environmentOf(s claudesessions.Session) string {
	if strings.TrimSpace(s.Launcher) != "" {
		return s.Launcher
	}
	return s.HomeLabel
}

// matches filtra por lo que se ve en la fila: título, carpeta, rama y home.
func matches(s claudesessions.Session, q string) bool {
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{
		s.Title, s.Cwd, s.Branch, s.HomeLabel, s.Project, s.ID}, " "))
	for _, term := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

func (s *Server) claudeSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.claude.sessions()
	if err != nil && len(list) == 0 {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 60
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	attached := map[string]bool{}
	if marked, err := s.reg.List(); err == nil {
		for _, e := range marked {
			attached[e.SessionID] = true
		}
	}
	type row struct {
		claudesessions.Session
		Attached    bool   `json:"attached"`
		Environment string `json:"environment"`
	}
	// Los conteos se calculan sobre lo que pasa la búsqueda pero antes del
	// filtro de entorno: si no, al elegir uno los demás se irían a cero y no
	// habría cómo volver.
	env := strings.TrimSpace(r.URL.Query().Get("env"))
	counts := map[string]int{}
	out := make([]row, 0, limit)
	matched := 0
	for _, item := range list {
		if !matches(item, q) {
			continue
		}
		name := environmentOf(item)
		counts[name]++
		matched++
		if env != "" && name != env {
			continue
		}
		if len(out) < limit {
			out = append(out, row{Session: item, Attached: attached[item.ID], Environment: name})
		}
	}
	type envCount struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	envs := make([]envCount, 0, len(counts))
	for name, n := range counts {
		envs = append(envs, envCount{Name: name, Count: n})
	}
	sort.Slice(envs, func(i, j int) bool {
		if envs[i].Count != envs[j].Count {
			return envs[i].Count > envs[j].Count
		}
		return envs[i].Name < envs[j].Name
	})
	writeJSON(w, map[string]any{
		"sessions": out, "total": len(list), "matched": matched, "environments": envs,
	})
}

// claudeAttach autoriza una sesión de Claude Code. Guarda el home donde vive
// porque sin él no hay forma de reanudarla desde otro lado.
func (s *Server) claudeAttach(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	found, ok := s.claude.find(strings.TrimSpace(body.SessionID))
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no se encontró la sesión %q", body.SessionID))
		return
	}
	entry, err := s.reg.Mark(r.Context(), schedulable.Entry{
		Kind:        schedulable.KindClaude,
		SessionID:   found.ID,
		SessionPath: found.Path,
		WorkDir:     found.Cwd,
		ConfigHome:  found.Home,
		Title:       found.Title,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, entry)
}

// claudeOpen abre en una terminal una sesión del listado, esté enganchada o no:
// para solo retomarla no hace falta autorizar nada.
func (s *Server) claudeOpen(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	found, ok := s.claude.find(strings.TrimSpace(body.SessionID))
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no se encontró la sesión %q", body.SessionID))
		return
	}
	err := OpenInTerminal(schedulable.Entry{
		Kind:       schedulable.KindClaude,
		SessionID:  found.ID,
		WorkDir:    found.Cwd,
		ConfigHome: found.Home,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"status": "opened"})
}

// claudeRefresh vacía el caché para que el próximo listado relea el disco.
func (s *Server) claudeRefresh(w http.ResponseWriter, r *http.Request) {
	if !guarded(w, r) {
		return
	}
	s.claude.invalidate()
	s.claudeSessions(w, r)
}
