package memory

import (
	"os"
	"path/filepath"

	"reasonix/internal/config"
)

// ClaudeHomeFor returns the Claude Code home that owns cwd's project memory:
// $CLAUDE_CONFIG_DIR when set, then ~/.claude-assistant, then ~/.claude.
// Empty when no candidate holds a memory store for this project, in which
// case callers keep Reasonix's own store.
func ClaudeHomeFor(cwd string) string {
	if dir := cleanHomeEnv("CLAUDE_CONFIG_DIR"); dir != "" && claudeHomeHasMemory(dir, cwd) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(home, ".claude-assistant"), // home de "negocio" activado por CLAUDE_CONFIG_DIR
		filepath.Join(home, ".claude"),           // home por defecto de Claude Code
	} {
		if claudeHomeHasMemory(candidate, cwd) {
			return candidate
		}
	}
	return ""
}

// claudeHomeHasMemory reports whether home holds a non-empty memory store for
// the project at cwd. An empty directory is treated as absent so a dead home
// never shadows a live one.
func claudeHomeHasMemory(home, cwd string) bool {
	entries, err := os.ReadDir(filepath.Join(home, "projects", config.WorkspaceSlug(absOf(cwd)), "memory"))
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func cleanHomeEnv(key string) string {
	dir := os.Getenv(key)
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}
