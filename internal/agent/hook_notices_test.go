package agent

import (
	"strings"
	"testing"
)

func TestAttachHookNoticesEmpty(t *testing.T) {
	body, notice := attachHookNotices("result ok", nil)
	if body != "result ok" || notice != "" {
		t.Fatalf("empty notices must leave body untouched, got body=%q notice=%q", body, notice)
	}
}

func TestAttachHookNoticesAppends(t *testing.T) {
	body, notice := attachHookNotices("result ok", []string{"hook [global/PostToolUse] warn: limite duro"})
	if !strings.Contains(body, "result ok") {
		t.Errorf("body lost the tool result: %q", body)
	}
	if !strings.Contains(body, "limite duro") {
		t.Errorf("body missing the hook notice: %q", body)
	}
	if notice != "" {
		t.Errorf("short notices should not truncate, got notice %q", notice)
	}
}

func TestAttachHookNoticesCapsVerbose(t *testing.T) {
	notices := []string{strings.Repeat("x", maxHookNoticeBytes+100)}
	body, _ := attachHookNotices("ok", notices)
	if !strings.Contains(body, "[hook notices truncated]") {
		t.Error("verbose notices must be capped with a truncation marker")
	}
}
