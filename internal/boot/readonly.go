package boot

import (
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// applySessionReadOnly bounds a session at the registry: writers are refused
// and bash keeps only read-only commands. It lands after the registry is
// assembled and before the executor sees it, so sub-agent and skill registries
// derived from the same one inherit the bound.
func applySessionReadOnly(reg *tool.Registry, sink event.Sink, enabled bool) {
	if !enabled {
		return
	}
	dropped := reg.SetAdmission(agent.SessionReadOnlyAdmission())
	if len(dropped) == 0 {
		return
	}
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text:   "Read-only session: writer tools are not available.",
		Detail: "read-only session dropped: " + strings.Join(dropped, ", ")})
}
