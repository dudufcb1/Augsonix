package codesearch

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"reasonix/internal/fileutil"
)

// stateFileName guarda el hash con el que se indexó cada archivo; es lo único
// que separa un reindexado incremental de uno completo.
const stateFileName = "files.json"

// State recuerda con qué contenido se indexó cada archivo. A diferencia del
// índice de vectores no se puede reconstruir: si se pierde, todo el workspace
// se vuelve a embeber (y a cobrar).
type State struct {
	// mu protege files: durante un escaneo varios archivos se indexan a la vez
	// y todos registran aquí su hash. Sin candado, dos escrituras simultáneas
	// al mapa tumban el proceso.
	mu    sync.RWMutex
	path  string
	files map[string]string
}

// LoadState lee el estado guardado en dir. Un archivo ausente o corrupto
// devuelve un estado vacío en vez de error: reindexar de más es recuperable,
// negarse a arrancar no.
func LoadState(dir string) (*State, error) {
	s := &State{path: filepath.Join(dir, stateFileName), files: map[string]string{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, nil
	}
	if err := json.Unmarshal(data, &s.files); err != nil {
		s.files = map[string]string{}
	}
	return s, nil
}

// Hash devuelve el hash con el que se indexó path, y si estaba registrado.
func (s *State) Hash(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.files[path]
	return h, ok
}

// Unchanged reporta si path ya está indexado con ese mismo contenido.
func (s *State) Unchanged(path, hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prev, ok := s.files[path]
	return ok && prev == hash
}

// Set registra el hash con el que quedó indexado path.
func (s *State) Set(path, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = hash
}

// Delete olvida path, para que un archivo borrado no quede marcado como vigente.
func (s *State) Delete(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, path)
}

// Paths devuelve los archivos registrados, ordenados para que el barrido de
// huérfanos sea determinista.
func (s *State) Paths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.files))
	for p := range s.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Len es la cantidad de archivos indexados.
func (s *State) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
}

// Clear olvida todo, dejando el siguiente escaneo como uno inicial.
func (s *State) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = map[string]string{}
}

// Save persiste el estado. Se llama al cerrar un lote, no por archivo: en un
// escaneo inicial serían miles de escrituras del mismo archivo.
func (s *State) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.mu.RLock()
	data, err := json.Marshal(s.files)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(s.path, data, 0o644)
}
