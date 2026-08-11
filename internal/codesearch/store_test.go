package codesearch

import (
	"os"
	"path/filepath"
	"testing"
)

// chunkAt arma un chunk de prueba con una ruta y un texto identificables.
func chunkAt(path, content string) Chunk {
	return Chunk{Path: path, StartLine: 1, EndLine: 5, Content: content, Hash: segmentHash(path, 1, 5, content)}
}

func TestStoreSearchRanksNearestVectorFirst(t *testing.T) {
	// La búsqueda ordena por similitud coseno: el vector idéntico a la consulta
	// tiene que salir antes que uno ortogonal.
	s, _ := OpenLocalStore(t.TempDir(), "test-model", 3)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "alpha")}, []int8{100, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace("b.go", []Chunk{chunkAt("b.go", "beta")}, []int8{0, 100, 0}); err != nil {
		t.Fatal(err)
	}

	got := s.Search([]int8{100, 0, 0}, 2)
	if len(got) != 2 {
		t.Fatalf("esperaba 2 resultados, hubo %d", len(got))
	}
	if got[0].Chunk.Path != "a.go" {
		t.Errorf("primer resultado = %q, esperaba a.go", got[0].Chunk.Path)
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("scores sin ordenar: %v <= %v", got[0].Score, got[1].Score)
	}
}

func TestStoreSearchRespectsLimit(t *testing.T) {
	// El límite acota cuántos candidatos pasan al reranker, que es la etapa que
	// sí cuesta dinero por documento.
	s, _ := OpenLocalStore(t.TempDir(), "test-model", 2)
	for _, p := range []string{"a.go", "b.go", "c.go"} {
		if err := s.Replace(p, []Chunk{chunkAt(p, p)}, []int8{50, 50}); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.Search([]int8{50, 50}, 2); len(got) != 2 {
		t.Errorf("devolvió %d resultados, el límite era 2", len(got))
	}
}

func TestStoreReplaceSwapsFileContents(t *testing.T) {
	// Reindexar un archivo editado sustituye sus chunks; si se acumularan,
	// la búsqueda devolvería líneas que ya no existen en disco.
	s, _ := OpenLocalStore(t.TempDir(), "test-model", 2)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "viejo")}, []int8{10, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "nuevo")}, []int8{0, 10}); err != nil {
		t.Fatal(err)
	}
	_, chunks := s.Stats()
	if chunks != 1 {
		t.Fatalf("quedaron %d chunks tras reemplazar, esperaba 1", chunks)
	}
	if got := s.Search([]int8{0, 10}, 1); got[0].Chunk.Content != "nuevo" {
		t.Errorf("contenido = %q, esperaba el nuevo", got[0].Chunk.Content)
	}
}

func TestStoreReplaceRejectsMismatchedVectorCount(t *testing.T) {
	// Un desajuste entre chunks y valores significa que la respuesta del
	// embebedor vino incompleta: mejor error que un índice desalineado.
	s, _ := OpenLocalStore(t.TempDir(), "test-model", 4)
	err := s.Replace("a.go", []Chunk{chunkAt("a.go", "x"), chunkAt("a.go", "y")}, []int8{1, 2, 3, 4})
	if err == nil {
		t.Error("esperaba error por cantidad de valores incorrecta")
	}
}

func TestStoreDeleteRemovesFile(t *testing.T) {
	// Un archivo borrado del workspace sale del índice, o la búsqueda seguiría
	// proponiendo rutas que ya no existen.
	s, _ := OpenLocalStore(t.TempDir(), "test-model", 2)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "x")}, []int8{10, 10}); err != nil {
		t.Fatal(err)
	}
	s.Delete("a.go")
	if s.Has("a.go") {
		t.Error("el archivo sigue en el índice tras borrarlo")
	}
	if got := s.Search([]int8{10, 10}, 5); len(got) != 0 {
		t.Errorf("la búsqueda devolvió %d resultados de un índice vacío", len(got))
	}
}

func TestStoreRoundTripsThroughDisk(t *testing.T) {
	// Al reabrir, chunks y vectores tienen que llegar intactos: de eso depende
	// que arrancar una sesión nueva no vuelva a embeber el repo completo.
	dir := t.TempDir()
	s, _ := OpenLocalStore(dir, "test-model", 3)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "alpha"), chunkAt("a.go", "beta")}, []int8{100, 0, 0, 0, 100, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace("b.go", []Chunk{chunkAt("b.go", "gamma")}, []int8{0, 0, 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save devolvió error: %v", err)
	}

	again, _ := OpenLocalStore(dir, "test-model", 3)
	files, chunks := again.Stats()
	if files != 2 || chunks != 3 {
		t.Fatalf("se recuperaron %d archivos y %d chunks, esperaba 2 y 3", files, chunks)
	}
	got := again.Search([]int8{0, 0, 100}, 1)
	if len(got) != 1 || got[0].Chunk.Content != "gamma" {
		t.Errorf("la búsqueda tras recargar no encontró el vector esperado: %+v", got)
	}
}

func TestOpenLocalStoreDiscardsIndexFromAnotherModel(t *testing.T) {
	// Vectores de modelos distintos no son comparables. Abrir con otro modelo
	// tiene que empezar de cero en vez de mezclar espacios vectoriales.
	dir := t.TempDir()
	s, _ := OpenLocalStore(dir, "voyage-code-3", 3)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "x")}, []int8{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	other, _ := OpenLocalStore(dir, "voyage-4", 3)
	if files, _ := other.Stats(); files != 0 {
		t.Errorf("se reusó un índice de otro modelo (%d archivos)", files)
	}
}

func TestOpenLocalStoreDiscardsIndexWithOtherDimension(t *testing.T) {
	// Cambiar la dimensión configurada invalida el índice igual que cambiar de
	// modelo: los vectores guardados ya no encajan con los nuevos.
	dir := t.TempDir()
	s, _ := OpenLocalStore(dir, "voyage-code-3", 3)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "x")}, []int8{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	other, _ := OpenLocalStore(dir, "voyage-code-3", 4)
	if files, _ := other.Stats(); files != 0 {
		t.Errorf("se reusó un índice de otra dimensión (%d archivos)", files)
	}
}

func TestOpenLocalStoreDiscardsTruncatedVectors(t *testing.T) {
	// Si el proceso murió entre escribir vectores y manifiesto, el blob queda
	// corto: hay que reindexar en vez de servir chunks con vectores ajenos.
	dir := t.TempDir()
	s, _ := OpenLocalStore(dir, "test-model", 3)
	if err := s.Replace("a.go", []Chunk{chunkAt("a.go", "x"), chunkAt("a.go", "y")}, []int8{1, 2, 3, 4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, vectorsFileName), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	again, _ := OpenLocalStore(dir, "test-model", 3)
	if files, _ := again.Stats(); files != 0 {
		t.Errorf("se cargó un índice con vectores truncados (%d archivos)", files)
	}
}
