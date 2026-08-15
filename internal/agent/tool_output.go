// Tool-result sizing: the first-visible cap that keeps one oversized result
// from inflating the model window, and the session-temp spill that preserves
// the full original on disk for on-demand reads.
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/sessiontemp"
)

const (
	// maxToolOutputBytes caps a single tool result before it goes into the
	// model's context. ~16KB is roughly 4K tokens — enough for a full file
	// read or a busy grep. Larger results spill their full output to a
	// session-private temp file (truncateToolOutputSpill), so the model reads
	// only the parts it needs.
	maxToolOutputBytes = 16 * 1024
	// maxHookNoticeBytes caps the combined post-tool hook notices appended to
	// a tool result, so a verbose hook cannot inflate the model context. It
	// rides the tail, which the bounded forms preserve even for huge results.
	maxHookNoticeBytes = 8 * 1024
)

// attachHookNotices appends post-tool hook notices to a tool body so the model
// sees hook feedback (Claude-style). Notices are kept short (maxHookNoticeBytes)
// and ride the tail, which truncateToolOutputSpill preserves even for huge
// results.
func attachHookNotices(ctx context.Context, m *sessiontemp.Manager, body string, notices []string) (string, string) {
	if len(notices) == 0 {
		return body, ""
	}
	joined := strings.Join(notices, "\n")
	if len(joined) > maxHookNoticeBytes {
		joined = joined[:maxHookNoticeBytes] + "\n…[hook notices truncated]"
	}
	return truncateToolOutputSpill(ctx, m, strings.TrimRight(body, "\n")+"\n\n"+joined, "", "")
}

// truncateToolOutput is the first-visible hard cap for a tool result. Under-cap
// bodies are returned byte-identical. Over-cap bodies keep a tool-aware head
// and tail under maxToolOutputBytes; the full original is stored separately as
// RawContent by the session writer. The bounded form is stable for the message
// lifetime and is never re-truncated by later maintenance.
func truncateToolOutput(s string) (string, string) {
	return truncateToolOutputFor(s, "", "")
}

// truncateToolOutputFor is the tool-aware first-visible limiter. toolName and
// toolCallID populate the truncation marker so the model can re-fetch.
func truncateToolOutputFor(s, toolName, toolCallID string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	head, tail, kept := truncationHeadTail(s, toolName, strategyForToolName(toolName))
	omitted := len(s) - kept
	namePart := toolName
	if namePart == "" {
		namePart = "tool"
	}
	idPart := toolCallID
	if idPart == "" {
		idPart = "-"
	}
	notice := fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	marker := fmt.Sprintf(
		"\n\n…[truncated tool=%s call_id=%s original_bytes=%d kept_bytes=%d — full original retained in canonical transcript; re-read or retry with narrower args]…\n\n",
		namePart, idPart, len(s), kept,
	)
	return fitToCap(head, marker, tail), notice
}

// truncateToolOutputSpill is the model-facing limiter for the execution path.
// Over-cap bodies write the full original to a session-private temp file and
// return a bounded preview whose first line carries the path, so the model
// reads only the parts it needs with grep/read_file. Falls back to the classic
// head/tail cut when no temp manager is available.
func truncateToolOutputSpill(ctx context.Context, m *sessiontemp.Manager, s, toolName, toolCallID string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	if path, ok := spillToolOutput(ctx, m, toolName, toolCallID, s); ok {
		return spillPreview(path, s, toolName, toolCallID)
	}
	return truncateToolOutputFor(s, toolName, toolCallID)
}

// spillToolOutput writes s to a file in the session-private temp directory and
// returns its absolute path. ok is false when no manager is available or the
// write fails; callers fall back to head/tail truncation.
func spillToolOutput(ctx context.Context, m *sessiontemp.Manager, toolName, toolCallID, s string) (string, bool) {
	if m == nil {
		m = sessiontemp.FromContext(ctx)
	}
	if m == nil {
		return "", false
	}
	lease, err := m.Acquire()
	if err != nil {
		return "", false
	}
	defer lease.Release()
	path := filepath.Join(lease.Dir(), spillFileName(toolName, toolCallID))
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		return "", false
	}
	return path, true
}

// spillFileName derives a stable per-call file name in the session temp dir.
// Tool call IDs are safe by construction; anything else is sanitized.
func spillFileName(toolName, toolCallID string) string {
	name := toolCallID
	if name == "" {
		name = toolName
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "tool_output.txt"
	}
	if b.Len() > 80 {
		return "tool_output_" + b.String()[:80] + ".txt"
	}
	return "tool_output_" + b.String() + ".txt"
}

// spillPreview builds the bounded tool result for a spilled output: the path on
// the first line (survives any later head-snip during compaction), then the
// tool-aware head/tail preview with a read-it-yourself marker in the middle.
func spillPreview(path, s, toolName, toolCallID string) (string, string) {
	head, tail, kept := truncationHeadTail(s, toolName, strategyForToolName(toolName))
	omitted := len(s) - kept
	first := fmt.Sprintf("tool output spilled to %s (%d bytes; %d elided from context)\n\n", path, len(s), omitted)
	marker := fmt.Sprintf(
		"\n\n…[full output at %s — not in context. Read only the parts you need: grep -n \"<pattern>\" %s, or read_file %s with offset/limit]…\n\n",
		path, path, path,
	)
	notice := fmt.Sprintf("tool output spilled: %d of %d bytes elided to %s", omitted, len(s), path)
	return fitToCap(first+head, marker, tail), notice
}

// strategyForToolName returns the head/tail sizing a tool's output shape wants:
// file reads front-load content, greps keep context lines around matches.
func strategyForToolName(toolName string) snipStrategy {
	switch {
	case toolName == "bash" || toolName == "shell" || strings.Contains(toolName, "bash"):
		return snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
	case toolName == "read_file" || toolName == "web_fetch" || strings.Contains(toolName, "read"):
		return snipStrategy{head: 120, tail: 12, headChars: 12000, tailChars: 2000}
	case toolName == "grep" || toolName == "glob" || toolName == "ls" || toolName == "list_dir":
		return snipStrategy{head: 80, tail: 8, headChars: 10000, tailChars: 1000}
	}
	return snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
}

// truncationHeadTail slices s into the head and tail a strategy wants, scaled
// down when the pair exceeds the hard cap, and biased toward the tail when the
// body looks like a failure.
func truncationHeadTail(s, toolName string, strategy snipStrategy) (head, tail string, kept int) {
	headKeep := strategy.headChars
	tailKeep := strategy.tailChars
	if headKeep+tailKeep > maxToolOutputBytes-512 {
		headKeep = maxToolOutputBytes * 2 / 3
		tailKeep = maxToolOutputBytes - headKeep - 512
	}
	if headKeep < 1024 {
		headKeep = maxToolOutputBytes / 2
		tailKeep = maxToolOutputBytes / 2
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "error:") || strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal:") {
		tailKeep = max(tailKeep, maxToolOutputBytes/3)
		if headKeep+tailKeep > maxToolOutputBytes-512 {
			headKeep = maxToolOutputBytes - 512 - tailKeep
		}
	}
	head = snapToRuneBoundary(s, 0, headKeep)
	tail = snapToRuneBoundary(s, len(s)-tailKeep, len(s))
	return head, tail, len(head) + len(tail)
}

// fitToCap trims head and tail (never the marker) so the bounded body fits
// maxToolOutputBytes. The marker's path survives even a hard overflow.
func fitToCap(head, marker, tail string) string {
	body := head + marker + tail
	if len(body) <= maxToolOutputBytes {
		return body
	}
	overflow := len(body) - maxToolOutputBytes
	trimHead := overflow / 2
	trimTail := overflow - trimHead
	if trimHead < len(head) {
		head = snapToRuneBoundary(head, 0, len(head)-trimHead)
	}
	if trimTail < len(tail) {
		tail = snapToRuneBoundary(tail, trimTail, len(tail))
	}
	return head + marker + tail
}

// snapToRuneBoundary returns s[lo:hi] with the bounds nudged outward until
// both land on rune-start positions.
func snapToRuneBoundary(s string, lo, hi int) string {
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}
