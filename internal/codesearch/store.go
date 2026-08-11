package codesearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"reasonix/internal/fileutil"
)

const (
	indexFileName   = "index.json"
	vectorsFileName = "vectors.bin"
)

// LocalStore guarda un vector por chunk y busca con un barrido lineal. No hay
// índice aproximado (HNSW, IVF) porque el binario se compila sin CGO y no hay
// extensión vectorial disponible; a escala de un repositorio el barrido cuesta
// milisegundos. Los vectores van en int8, cuatro veces menos memoria que
// float32, porque el orden fino no sale de aquí sino del reranker.
type LocalStore struct {
	// mu protege files: el watcher reindexa desde su goroutine mientras la
	// herramienta de búsqueda lee desde la del agente.
	mu    sync.RWMutex
	dir   string
	dims  int
	model string
	files map[string]*fileVectors
}

// fileVectors son los chunks de un archivo con sus vectores concatenados, así
// que reindexar un archivo es reemplazar una entrada y no recompactar todo.
type fileVectors struct {
	// Hash es el contenido con que se indexó el archivo, guardado junto a sus
	// vectores para poder saltárselo sin depender de un archivo de estado aparte.
	Hash   string    `json:"hash"`
	Chunks []Chunk   `json:"chunks"`
	vecs   []int8    // len == len(Chunks)*dims
	norms  []float32 // norma de cada vector, para el coseno
}

// Match es un chunk recuperado con su similitud, entre 0 y 1.
type Match struct {
	Chunk Chunk
	Score float32
}

type indexManifest struct {
	Dims   int                `json:"dims"`
	Model  string             `json:"model"`
	Files  map[string][]Chunk `json:"files"`
	Hashes map[string]string  `json:"hashes"`
	Order  []string           `json:"order"`
}

// OpenStore carga el índice de dir. Si no existe, o si fue construido con otro
// modelo o dimensión, devuelve uno vacío: mezclar vectores de modelos distintos
// da resultados sin sentido, y reindexar es preferible a mentir.
func OpenLocalStore(dir, model string, dims int) (*LocalStore, error) {
	s := &LocalStore{dir: dir, dims: dims, model: model, files: map[string]*fileVectors{}}
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, nil
	}
	var man indexManifest
	if err := json.Unmarshal(data, &man); err != nil || man.Dims != dims || man.Model != model {
		return s, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, vectorsFileName))
	if err != nil {
		return s, nil
	}
	if err := s.load(man, raw); err != nil {
		return &LocalStore{dir: dir, dims: dims, model: model, files: map[string]*fileVectors{}}, nil
	}
	return s, nil
}

// load reparte el blob de vectores entre los archivos del manifiesto siguiendo
// el mismo orden con que se escribió.
func (s *LocalStore) load(man indexManifest, raw []byte) error {
	offset := 0
	for _, path := range man.Order {
		chunks := man.Files[path]
		width := len(chunks) * s.dims
		if offset+width > len(raw) {
			return fmt.Errorf("vectors.bin truncado en %s", path)
		}
		fv := &fileVectors{Hash: man.Hashes[path], Chunks: chunks, vecs: bytesToInt8(raw[offset : offset+width])}
		fv.recomputeNorms(s.dims)
		s.files[path] = fv
		offset += width
	}
	return nil
}

// Replace deja el archivo con exactamente esos chunks y vectores, quitando lo
// que tuviera antes. vecs viene concatenado: dims valores por chunk.
func (s *LocalStore) Replace(path, fileHash string, chunks []Chunk, vecs []int8) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(chunks)*s.dims != len(vecs) {
		return fmt.Errorf("codesearch: %d chunks piden %d valores, llegaron %d", len(chunks), len(chunks)*s.dims, len(vecs))
	}
	if len(chunks) == 0 {
		delete(s.files, path)
		return nil
	}
	fv := &fileVectors{Hash: fileHash, Chunks: chunks, vecs: vecs}
	fv.recomputeNorms(s.dims)
	s.files[path] = fv
	return nil
}

// FileHash devuelve con qué contenido se indexó un archivo.
func (s *LocalStore) FileHash(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fv, ok := s.files[path]
	if !ok || fv.Hash == "" {
		return "", false
	}
	return fv.Hash, true
}

// Delete saca del índice todo lo que venía de un archivo.
func (s *LocalStore) Delete(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, path)
}

// Has reporta si un archivo tiene chunks en el índice.
func (s *LocalStore) Has(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.files[path]
	return ok
}

// Paths devuelve los archivos indexados, ordenados.
func (s *LocalStore) Paths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pathsLocked()
}

func (s *LocalStore) pathsLocked() []string {
	out := make([]string, 0, len(s.files))
	for p := range s.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Stats reporta cuántos archivos y chunks hay indexados.
func (s *LocalStore) Stats() (files, chunks int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, fv := range s.files {
		chunks += len(fv.Chunks)
	}
	return len(s.files), chunks
}

// Search devuelve los limit chunks más parecidos al vector de consulta,
// ordenados de mayor a menor similitud coseno.
func (s *LocalStore) Search(query []int8, limit int) []Match {
	if len(query) != s.dims || limit <= 0 {
		return nil
	}
	qNorm := norm(query)
	if qNorm == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Match
	for _, path := range s.pathsLocked() {
		fv := s.files[path]
		for i := range fv.Chunks {
			vec := fv.vecs[i*s.dims : (i+1)*s.dims]
			if fv.norms[i] == 0 {
				continue
			}
			score := float32(dot(query, vec)) / (qNorm * fv.norms[i])
			out = append(out, Match{Chunk: fv.Chunks[i], Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Save escribe manifiesto y vectores. El manifiesto va al final: si el proceso
// muere entre ambos, al abrir se detecta el blob truncado y se reindexa, en vez
// de servir un índice que apunta a vectores que no existen.
func (s *LocalStore) Save() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	s.mu.RLock()
	order := s.pathsLocked()
	man := indexManifest{Dims: s.dims, Model: s.model, Files: map[string][]Chunk{}, Hashes: map[string]string{}, Order: order}
	var blob []byte
	for _, path := range order {
		fv := s.files[path]
		man.Files[path] = fv.Chunks
		man.Hashes[path] = fv.Hash
		blob = append(blob, int8ToBytes(fv.vecs)...)
	}
	s.mu.RUnlock()
	if err := fileutil.AtomicWriteFile(filepath.Join(s.dir, vectorsFileName), blob, 0o644); err != nil {
		return err
	}
	data, err := json.Marshal(man)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(s.dir, indexFileName), data, 0o644)
}

func (fv *fileVectors) recomputeNorms(dims int) {
	fv.norms = make([]float32, len(fv.Chunks))
	for i := range fv.Chunks {
		fv.norms[i] = norm(fv.vecs[i*dims : (i+1)*dims])
	}
}

func dot(a, b []int8) int32 {
	var sum int32
	for i := range a {
		sum += int32(a[i]) * int32(b[i])
	}
	return sum
}

func norm(v []int8) float32 {
	var sum int32
	for _, x := range v {
		sum += int32(x) * int32(x)
	}
	return float32(math.Sqrt(float64(sum)))
}

func bytesToInt8(b []byte) []int8 {
	out := make([]int8, len(b))
	for i, v := range b {
		out[i] = int8(v)
	}
	return out
}

func int8ToBytes(v []int8) []byte {
	out := make([]byte, len(v))
	for i, x := range v {
		out[i] = byte(x)
	}
	return out
}

// Dims es la dimensión de los vectores del índice.
func (s *LocalStore) Dims() int { return s.dims }

var _ VectorStore = (*LocalStore)(nil)
