package agent

import (
	"path/filepath"
	"strings"
	"time"
)

// gatewaySessionID returns the stable logical-session identifier sent to
// gateways that group requests by it (opencode.ai reads x-opencode-session;
// the usage dashboard shows the last 8 chars of the stored value). The
// project slug goes last so that tail is human-readable, prefixed by a
// session timestamp for per-session stickiness. Empty omits the header.
func (a *Agent) gatewaySessionID() string {
	return gatewaySessionID(a.SessionPath(), a.writeWorkspaceRoot)
}

func gatewaySessionID(sessionPath, workspaceRoot string) string {
	slug := projectSlug(workspaceRoot)
	if slug == "" {
		return ""
	}
	ts := sessionTimestamp(sessionPath)
	if ts == "" {
		ts = time.Now().UTC().Format("01021504")
	}
	return "rx-" + ts + "-" + slug
}

// projectSlug condenses a workspace path into the first three letters of each
// word of its basename (DeepSeek-Reasonix -> dep-rea), at most three words, so
// the gateway dashboard tail stays short but recognizable.
func projectSlug(workspaceRoot string) string {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(workspaceRoot)))
	if base == "." || base == "/" || base == "" {
		return ""
	}
	var words []string
	for _, part := range splitNonAlnum(base) {
		if part == "" {
			continue
		}
		letters := []rune(strings.ToLower(part))
		cut := min(len(letters), 3)
		words = append(words, string(letters[:cut]))
		if len(words) == 3 {
			break
		}
	}
	return strings.Join(words, "-")
}

// sessionTimestamp extracts MMDDHHMM from a session file basename shaped like
// "20060102-150405.000000000-<model>.jsonl" (NewSessionPath). Empty when the
// path does not carry a parseable timestamp.
func sessionTimestamp(sessionPath string) string {
	base := filepath.Base(sessionPath)
	dot := strings.IndexByte(base, '.')
	if dot > 0 {
		base = base[:dot]
	}
	if len(base) < 15 {
		return ""
	}
	t, err := time.Parse("20060102-150405", base[:15])
	if err != nil {
		return ""
	}
	return t.UTC().Format("01021504")
}

// splitNonAlnum splits s on every run of non-alphanumeric characters.
func splitNonAlnum(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}
