package agent

import "reasonix/internal/tool"

// SessionReadOnlyAdmission returns the tool-registry admission filter for a
// session declared read-only. bash keeps its name and schema shape but runs
// only permission-classified read-only foreground commands; every other
// writer-capable tool is refused, late MCP arrivals included. The bound is the
// registry itself, not a permission rule, so no approval mode can lift it.
func SessionReadOnlyAdmission() func(tool.Tool) tool.Tool {
	return func(t tool.Tool) tool.Tool {
		if t == nil || t.ReadOnly() {
			return t
		}
		if t.Name() == "bash" {
			return readOnlyBash{inner: t}
		}
		return nil
	}
}
