package config

import (
	"strings"
	"testing"
)

// Mismo bug que [codesearch]: si el renderer no emite la sección, el guardado
// canónico borra el flag y reactiva la serialización de escritores sin avisar.
func TestConcurrentWritersSurvivesARenderAndReloadRoundTrip(t *testing.T) {
	var cfg Config
	cfg.Workspace.ConcurrentWriters = true

	rendered := RenderTOML(&cfg)
	if !strings.Contains(rendered, "[workspace]") || !strings.Contains(rendered, "concurrent_writers = true") {
		t.Fatalf("el config guardado no lleva [workspace] concurrent_writers:\n%s", rendered)
	}

	var reloaded Config
	if _, err := decodeTOMLBytes([]byte(rendered), &reloaded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reloaded.ConcurrentWriters() {
		t.Error("concurrent_writers se perdió: la serialización de escritores se reactivaría")
	}
}

func TestDefaultWorkspaceIsNotWrittenOut(t *testing.T) {
	var cfg Config
	if strings.Contains(RenderTOML(&cfg), "[workspace]") {
		t.Error("se escribió la sección [workspace] aunque está en su valor por defecto")
	}
	if strings.Contains(RenderTOMLProjectDelta(&cfg), "[workspace]") {
		t.Error("el delta escribió [workspace] aunque está en su valor por defecto")
	}
}

func TestConcurrentWritersDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := decodeTOMLBytes([]byte("[workspace]\nconcurrent_writers = true\n"), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.ConcurrentWriters() {
		t.Error("concurrent_writers = true no se reflejó en el accessor")
	}
	var absent Config
	if _, err := decodeTOMLBytes([]byte(""), &absent); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if absent.ConcurrentWriters() {
		t.Error("el accessor reportó true sin sección [workspace]")
	}
}
