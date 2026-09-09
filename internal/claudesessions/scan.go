// Package claudesessions lista las sesiones de Claude Code repartidas entre
// varios homes de configuración. Cada home es un CLAUDE_CONFIG_DIR distinto
// (el principal, el del asistente, los de otros modelos), y sus transcripts
// viven en <home>/projects/<slug>/<session-id>.jsonl.
//
// Reanudar una sesión exige saber de qué home salió, que es justo el dato que
// se pierde cuando uno abre sesiones en varios lados.
package claudesessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// headerLines es cuánto se lee de cada transcript. El título aparece en las
// primeras decenas de líneas y el cwd todavía antes, así que no hace falta
// abrir archivos de veinte megas enteros para listarlos.
const headerLines = 150

// maxLineBytes descarta líneas gigantes (imágenes en base64) sin cargarlas.
const maxLineBytes = 256 * 1024

// Session es una sesión de Claude Code lista para reanudar.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Home      string    `json:"home"`
	HomeLabel string    `json:"home_label"`
	Launcher  string    `json:"launcher,omitempty"`
	Project   string    `json:"project"`
	Cwd       string    `json:"cwd"`
	Branch    string    `json:"branch"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Updated   time.Time `json:"updated"`
}

// DiscoverHomes busca los homes de Claude Code bajo root. Un home es cualquier
// carpeta con un projects/ que contenga transcripts, y así aparecen tanto
// ~/.claude como los alternos sin tener que enumerarlos a mano.
func DiscoverHomes(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var homes []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.Contains(name, "claude") {
			continue
		}
		dir := filepath.Join(root, name, "projects")
		if hasTranscripts(dir) {
			homes = append(homes, filepath.Join(root, name))
		}
	}
	sort.Strings(homes)
	return homes
}

func hasTranscripts(projectsDir string) bool {
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		return false
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, p.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".jsonl") {
				return true
			}
		}
	}
	return false
}

// HomeLabel es el nombre corto con el que se distingue un home en la interfaz.
func HomeLabel(home string) string {
	base := strings.TrimPrefix(filepath.Base(home), ".")
	if base == "claude" {
		return "principal"
	}
	return strings.TrimPrefix(strings.TrimPrefix(base, "claude-"), "claude")
}

// List reúne las sesiones de todos los homes, de la más reciente a la más
// vieja. Un transcript ilegible se omite en vez de tumbar el listado.
func List(homes []string, limit int) ([]Session, error) {
	var out []Session
	for _, home := range homes {
		sessions, err := listHome(home)
		if err != nil {
			continue
		}
		out = append(out, sessions...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func listHome(home string) ([]Session, error) {
	projectsDir := filepath.Join(home, "projects")
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}
	label := HomeLabel(home)
	var out []Session
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, p.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil || info.Size() == 0 {
				continue
			}
			path := filepath.Join(dir, f.Name())
			s := Session{
				ID:        strings.TrimSuffix(f.Name(), ".jsonl"),
				Home:      home,
				HomeLabel: label,
				Project:   p.Name(),
				Path:      path,
				Size:      info.Size(),
				Updated:   info.ModTime(),
			}
			readHeader(path, &s)
			if s.Title == "" {
				s.Title = "(sin título)"
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// readHeader saca título, carpeta y rama del arranque del transcript. El último
// ai-title gana, porque el título se regenera conforme avanza la conversación.
func readHeader(path string, s *Session) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for i := 0; i < headerLines; i++ {
		line, err := readLine(r)
		if len(line) > 0 {
			applyHeaderLine(line, s)
		}
		if err != nil {
			return
		}
	}
}

func applyHeaderLine(line []byte, s *Session) {
	var row struct {
		Type    string `json:"type"`
		AITitle string `json:"aiTitle"`
		Cwd     string `json:"cwd"`
		Branch  string `json:"gitBranch"`
	}
	if err := json.Unmarshal(line, &row); err != nil {
		return
	}
	if row.Type == "ai-title" && strings.TrimSpace(row.AITitle) != "" {
		s.Title = row.AITitle
	}
	if s.Cwd == "" && row.Cwd != "" {
		s.Cwd = row.Cwd
	}
	if s.Branch == "" && row.Branch != "" {
		s.Branch = row.Branch
	}
}

// readLine devuelve una línea completa, descartando las que pasan del límite
// para no cargar en memoria un adjunto en base64.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, more, err := r.ReadLine()
		if len(buf)+len(chunk) <= maxLineBytes {
			buf = append(buf, chunk...)
		} else {
			buf = nil
		}
		if err != nil {
			return buf, err
		}
		if !more {
			return buf, nil
		}
	}
}

// ResumeEnv es la variable que apunta al home donde vive la sesión. Sin ella
// Claude Code busca en el home principal y no encuentra nada.
const ResumeEnv = "CLAUDE_CONFIG_DIR"

// ResumeCommand arma la invocación que reanuda la sesión con un encargo.
// binary vacío usa "claude".
func ResumeCommand(binary string, s Session, prompt string, extra ...string) (env []string, args []string) {
	if strings.TrimSpace(binary) == "" {
		binary = "claude"
	}
	env = []string{fmt.Sprintf("%s=%s", ResumeEnv, s.Home)}
	args = append([]string{"--resume", s.ID, "--dangerously-skip-permissions"}, extra...)
	return env, append(args, "-p", prompt)
}

// DefaultHome es el home que Claude Code usa cuando nadie le dice otra cosa.
func DefaultHome() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, ".claude")
}

// NeedsConfigHome dice si hay que exportar CLAUDE_CONFIG_DIR. Para el home
// principal la respuesta es no: el binario arma su almacenamiento sin esa
// variable al arrancar, y pasársela le hace ignorar la configuración existente
// y volver a la pantalla de bienvenida en vez de reanudar la sesión.
func NeedsConfigHome(home string) bool {
	home = strings.TrimRight(strings.TrimSpace(home), "/")
	if home == "" {
		return false
	}
	return home != strings.TrimRight(DefaultHome(), "/")
}
