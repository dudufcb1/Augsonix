package config

// MemoryConfig controls how Reasonix reads the memories Claude Code saved for
// the current project. ClaudeStore enables that bridge in read-only mode:
// Reasonix never writes to a Claude home.
type MemoryConfig struct {
	ClaudeStore bool `toml:"claude_store"`
}

// ClaudeStoreEnabled reports whether the auto-memory store should read from
// the Claude Code home that owns the current project instead of Reasonix's
// own store.
func (c *Config) ClaudeStoreEnabled() bool {
	return c != nil && c.Memory.ClaudeStore
}
