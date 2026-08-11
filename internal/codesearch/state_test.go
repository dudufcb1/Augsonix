package codesearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStateMissingFileStartsEmpty(t *testing.T) {
	// Un workspace que nunca se indexó no tiene archivo de estado; arrancar
	// vacío es lo correcto, y fallar dejaría el índice inservible.
	s, err := LoadState(t.TempDir())
	if err != nil {
		t.Fatalf("LoadState devolvió error: %v", err)
	}
	if s.Len() != 0 {
		t.Errorf("estado inicial con %d archivos, esperaba 0", s.Len())
	}
}

func TestLoadStateCorruptFileStartsEmpty(t *testing.T) {
	// Si el JSON quedó truncado (corte de luz a media escritura), reindexar
	// todo es recuperable; negarse a arrancar no lo es.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte("{no es json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState devolvió error: %v", err)
	}
	if s.Len() != 0 {
		t.Errorf("estado corrupto dejó %d archivos, esperaba 0", s.Len())
	}
}

func TestStateRoundTripsThroughDisk(t *testing.T) {
	// Lo guardado se recupera igual en la siguiente sesión: de eso depende que
	// el segundo arranque no vuelva a embeber el workspace completo.
	dir := t.TempDir()
	s, _ := LoadState(dir)
	s.Set("internal/agent/agent.go", "hash-a")
	s.Set("internal/cli/main.go", "hash-b")
	if err := s.Save(); err != nil {
		t.Fatalf("Save devolvió error: %v", err)
	}

	again, _ := LoadState(dir)
	if again.Len() != 2 {
		t.Fatalf("se recuperaron %d archivos, esperaba 2", again.Len())
	}
	if !again.Unchanged("internal/agent/agent.go", "hash-a") {
		t.Error("el archivo guardado no se reporta como sin cambios")
	}
	if again.Unchanged("internal/agent/agent.go", "hash-distinto") {
		t.Error("un hash distinto se reportó como sin cambios")
	}
}

func TestStateDeleteForgetsFile(t *testing.T) {
	// Al borrar un archivo del workspace hay que olvidarlo, o el barrido de
	// huérfanos lo seguiría dando por vigente y sus chunks quedarían colgados.
	s, _ := LoadState(t.TempDir())
	s.Set("gone.go", "hash")
	s.Delete("gone.go")
	if _, ok := s.Hash("gone.go"); ok {
		t.Error("el archivo borrado sigue registrado")
	}
}

func TestStatePathsAreSorted(t *testing.T) {
	// El orden estable hace que el barrido de huérfanos sea reproducible entre
	// corridas, que es lo que permite comparar resultados en un test.
	s, _ := LoadState(t.TempDir())
	for _, p := range []string{"c.go", "a.go", "b.go"} {
		s.Set(p, "h")
	}
	got := s.Paths()
	want := []string{"a.go", "b.go", "c.go"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Paths() = %v, esperaba %v", got, want)
		}
	}
}
