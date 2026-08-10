package openai

import "net/http"

// ApplyOpenCodeSessionHeader forwards the logical-session identifier to
// gateways that group requests by it. opencode.ai reads x-opencode-session
// and its usage dashboard shows the last 8 chars of the stored value, so
// callers pass a stable per-session ID whose tail is human-readable.
func ApplyOpenCodeSessionHeader(h http.Header, targetURL, sessionID string) {
	if IsOpenCode(targetURL) && sessionID != "" {
		h.Set("x-opencode-session", sessionID)
	}
}
