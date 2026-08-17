package memory

import (
	"path/filepath"
	"testing"
)

// seedClaudeStore siembra un fact en el store de memoria de un home de Claude
// falso para el proyecto en cwd, con el mismo mecanismo que usa el detector.
func seedClaudeStore(t *testing.T, home, cwd string) {
	t.Helper()
	s := StoreFor(home, cwd)
	if _, err := s.Save(Memory{
		Name:        "facto-prueba",
		Title:       "Facto de prueba",
		Description: "un hecho",
		Type:        TypeProject,
		Body:        "cuerpo del hecho",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeHomeForPrefersConfigDir(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "proyecto")
	claudeHome := filepath.Join(base, "custom-claude")
	seedClaudeStore(t, claudeHome, cwd)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	if got := ClaudeHomeFor(cwd); got != claudeHome {
		t.Fatalf("ClaudeHomeFor = %q, want %q", got, claudeHome)
	}
}

func TestClaudeHomeForIgnoresConfigDirWithoutMemory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	cwd := filepath.Join(base, "proyecto")
	// CLAUDE_CONFIG_DIR apunta a un home que no tiene memoria para el proyecto.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(base, "sin-memoria"))
	assistantHome := filepath.Join(base, ".claude-assistant")
	seedClaudeStore(t, assistantHome, cwd)

	if got := ClaudeHomeFor(cwd); got != assistantHome {
		t.Fatalf("ClaudeHomeFor = %q, want %q", got, assistantHome)
	}
}

func TestClaudeHomeForFallsToDefaultClaudeHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	cwd := filepath.Join(base, "proyecto")
	claudeHome := filepath.Join(base, ".claude")
	seedClaudeStore(t, claudeHome, cwd)

	if got := ClaudeHomeFor(cwd); got != claudeHome {
		t.Fatalf("ClaudeHomeFor = %q, want %q", got, claudeHome)
	}
}

func TestClaudeHomeForEmptyWhenNoHomeHasMemory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(base, "sin-memoria"))
	cwd := filepath.Join(base, "proyecto")

	if got := ClaudeHomeFor(cwd); got != "" {
		t.Fatalf("ClaudeHomeFor = %q, want empty", got)
	}
}
