package config

// WorkspaceConfig controls how a workspace coordinates concurrent sessions.
// ConcurrentWriters opts a container workspace (a root holding many independent
// projects) out of write-lease serialization; sessions then mutate it in
// parallel, so edits to a shared path are not serialized.
type WorkspaceConfig struct {
	ConcurrentWriters bool `toml:"concurrent_writers"`
}

// ConcurrentWriters reports whether the workspace opts out of write-lease serialization.
func (c *Config) ConcurrentWriters() bool {
	return c != nil && c.Workspace.ConcurrentWriters
}
