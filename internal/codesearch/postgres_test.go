package codesearch

import (
	"context"
	"os"
	"testing"
)

// pgStore abre el almacén contra la base de pruebas, o salta el test. La
// variable la provee quien corre la integración; sin ella no hay servidor.
func pgStore(t *testing.T, workspace string, dims int) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("CODESEARCH_POSTGRES_URL")
	if dsn == "" {
		t.Skip("sin CODESEARCH_POSTGRES_URL")
	}
	s, err := OpenPostgresStore(context.Background(), dsn, workspace, workspace, "test-model", dims)
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	t.Cleanup(func() {
		for _, p := range s.Paths() {
			s.Delete(p)
		}
		s.Close()
	})
	return s
}

func TestPostgresRoundTripsChunks(t *testing.T) {
	// Lo guardado se recupera con su ubicación intacta: de eso depende que un
	// resultado se pueda abrir sin volver a buscarlo.
	s := pgStore(t, "ws-roundtrip", 3)
	if err := s.Replace("internal/auth.go", "h", []Chunk{chunkAt("internal/auth.go", "func Authenticate() {}")}, []int8{100, 0, 0}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got := s.Search([]int8{100, 0, 0}, 5)
	if len(got) == 0 {
		t.Fatal("la búsqueda no devolvió nada")
	}
	if got[0].Chunk.Path != "internal/auth.go" || got[0].Chunk.Content != "func Authenticate() {}" {
		t.Errorf("volvió distinto de lo guardado: %+v", got[0].Chunk)
	}
}

func TestPostgresRanksNearestFirst(t *testing.T) {
	// El orden lo da la distancia coseno del índice: el vector idéntico a la
	// consulta tiene que salir antes que el ortogonal.
	s := pgStore(t, "ws-rank", 3)
	if err := s.Replace("a.go", "h", []Chunk{chunkAt("a.go", "alpha")}, []int8{100, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace("b.go", "h", []Chunk{chunkAt("b.go", "beta")}, []int8{0, 100, 0}); err != nil {
		t.Fatal(err)
	}
	got := s.Search([]int8{100, 0, 0}, 2)
	if len(got) != 2 {
		t.Fatalf("devolvió %d resultados, esperaba 2", len(got))
	}
	if got[0].Chunk.Path != "a.go" {
		t.Errorf("primero fue %q, esperaba a.go", got[0].Chunk.Path)
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("scores sin ordenar: %v <= %v", got[0].Score, got[1].Score)
	}
}

func TestPostgresReplaceSwapsFileContents(t *testing.T) {
	// Reindexar un archivo editado sustituye sus chunks; si se acumularan, la
	// búsqueda devolvería líneas que ya no existen en disco.
	s := pgStore(t, "ws-replace", 2)
	if err := s.Replace("a.go", "h", []Chunk{chunkAt("a.go", "viejo")}, []int8{10, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace("a.go", "h", []Chunk{chunkAt("a.go", "nuevo")}, []int8{0, 10}); err != nil {
		t.Fatal(err)
	}
	if _, chunks := s.Stats(); chunks != 1 {
		t.Fatalf("quedaron %d chunks tras reemplazar, esperaba 1", chunks)
	}
}

func TestPostgresKeepsWorkspacesApart(t *testing.T) {
	// Varios proyectos comparten servidor. Si se mezclaran, una búsqueda
	// devolvería código de otro repositorio, que es peor que no devolver nada.
	a := pgStore(t, "ws-uno", 2)
	b := pgStore(t, "ws-dos", 2)
	if err := a.Replace("a.go", "h", []Chunk{chunkAt("a.go", "del proyecto uno")}, []int8{50, 50}); err != nil {
		t.Fatal(err)
	}
	if err := b.Replace("b.go", "h", []Chunk{chunkAt("b.go", "del proyecto dos")}, []int8{50, 50}); err != nil {
		t.Fatal(err)
	}
	for _, r := range a.Search([]int8{50, 50}, 10) {
		if r.Chunk.Path != "a.go" {
			t.Errorf("se coló %q de otro workspace", r.Chunk.Path)
		}
	}
	if _, chunks := a.Stats(); chunks != 1 {
		t.Errorf("el workspace ve %d chunks, esperaba solo el suyo", chunks)
	}
}

func TestPostgresDeleteRemovesFile(t *testing.T) {
	// Un archivo borrado del disco sale del índice, o la búsqueda seguiría
	// proponiendo rutas que ya no existen.
	s := pgStore(t, "ws-delete", 2)
	if err := s.Replace("a.go", "h", []Chunk{chunkAt("a.go", "x")}, []int8{10, 10}); err != nil {
		t.Fatal(err)
	}
	s.Delete("a.go")
	if s.Has("a.go") {
		t.Error("el archivo sigue indexado tras borrarlo")
	}
}

func TestPostgresRejectsMismatchedVectorCount(t *testing.T) {
	// Un desajuste significa que la respuesta del embebedor vino incompleta:
	// mejor error que un índice desalineado.
	s := pgStore(t, "ws-mismatch", 4)
	err := s.Replace("a.go", "h", []Chunk{chunkAt("a.go", "x"), chunkAt("a.go", "y")}, []int8{1, 2, 3, 4})
	if err == nil {
		t.Error("aceptó una cantidad de valores incorrecta")
	}
}

func TestPostgresSupports2048Dimensions(t *testing.T) {
	// El caso que motivó el backend: 2048 excede el tope indexable del tipo
	// vector, y solo funciona porque el índice HNSW va sobre un cast a halfvec.
	s := pgStore(t, "ws-2048", 2048)
	vec := make([]int8, 2048)
	for i := range vec {
		vec[i] = int8(i % 100)
	}
	if err := s.Replace("big.go", "h", []Chunk{chunkAt("big.go", "vector grande")}, vec); err != nil {
		t.Fatalf("Replace con 2048 dimensiones: %v", err)
	}
	if got := s.Search(vec, 1); len(got) != 1 {
		t.Fatalf("la búsqueda con 2048 dimensiones devolvió %d resultados", len(got))
	}
}
