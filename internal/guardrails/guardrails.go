// Package guardrails centralizes the single switch that turns off the heuristic
// safety rails: the read-only intent parsed out of the user's own words, and the
// Auto Guard reviewer that second-guesses every mutation.
//
// They exist for unattended runs, where nobody is watching what the agent does.
// In a session where the operator reads every step before it lands they cost
// more than they protect: a phrase like "don't modify the auth test" is matched
// as a substring and silently turns the whole turn read-only, even though it was
// meant to exclude one file.
package guardrails

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// EnvVar turns the rails off for one process. Boot also sets it from the
// `guardrails` key of the config, so both routes end up here.
const EnvVar = "REASONIX_NO_GUARDRAILS"

var (
	once     sync.Once
	disabled bool
)

// Disabled reports whether the heuristic rails are off. Read once: the value is
// consulted on every tool call and must not turn a hot path into a syscall.
func Disabled() bool {
	once.Do(func() {
		disabled = truthy(os.Getenv(EnvVar))
	})
	return disabled
}

// Disable turns the rails off for this process. Boot calls it when the config
// asks for it, before anything reads Disabled.
func Disable() {
	_ = os.Setenv(EnvVar, "1")
}

func truthy(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		return parsed
	}
	// "on"/"yes" son las formas que la gente escribe cuando no piensa en Go.
	switch strings.ToLower(value) {
	case "on", "yes", "si", "sí":
		return true
	}
	return false
}
