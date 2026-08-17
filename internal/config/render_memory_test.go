package config

import (
	"strings"
	"testing"
)

// Mismo bug que [codesearch]/[workspace]: si el renderer no emite la sección,
// el guardado canónico borra el flag y Reasonix deja de leer las memorias de
// Claude sin avisar.
func TestClaudeStoreSurvivesARenderAndReloadRoundTrip(t *testing.T) {
	var cfg Config
	cfg.Memory.ClaudeStore = true

	rendered := RenderTOML(&cfg)
	if !strings.Contains(rendered, "[memory]") || !strings.Contains(rendered, "claude_store = true") {
		t.Fatalf("el config guardado no lleva [memory] claude_store:\n%s", rendered)
	}

	var reloaded Config
	if _, err := decodeTOMLBytes([]byte(rendered), &reloaded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reloaded.ClaudeStoreEnabled() {
		t.Error("claude_store se perdió: la lectura de memorias de Claude se apagaría")
	}
}

func TestDefaultMemoryIsNotWrittenOut(t *testing.T) {
	var cfg Config
	if strings.Contains(RenderTOML(&cfg), "[memory]") {
		t.Error("se escribió la sección [memory] aunque está en su valor por defecto")
	}
}

func TestClaudeStoreDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := decodeTOMLBytes([]byte("[memory]\nclaude_store = true\n"), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.ClaudeStoreEnabled() {
		t.Error("claude_store = true no se reflejó en el accessor")
	}
	var absent Config
	if _, err := decodeTOMLBytes([]byte(""), &absent); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if absent.ClaudeStoreEnabled() {
		t.Error("el accessor reportó true sin sección [memory]")
	}
}
