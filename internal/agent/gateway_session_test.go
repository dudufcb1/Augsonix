package agent

import (
	"strings"
	"testing"
)

func TestGatewaySessionID(t *testing.T) {
	cases := []struct {
		name        string
		sessionPath string
		workspace   string
		wantSlug    string
		wantEmpty   bool
	}{
		{name: "project with dash words", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "/home/edugoat/Dev/DeepSeek-Reasonix", wantSlug: "dee-rea"},
		{name: "single word", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "/home/edugoat/segurofeliz", wantSlug: "seg"},
		{name: "kebab project", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "/media/edugoat/Proyectos/usage-tracker", wantSlug: "usa-tra"},
		{name: "three words cap", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "/x/whatsapp-audio-transcriber", wantSlug: "wha-aud-tra"},
		{name: "no workspace", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "", wantEmpty: true},
		{name: "root workspace", sessionPath: "/s/20260808-180307.123456789-deepseek-v4-flash.jsonl", workspace: "/", wantEmpty: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gatewaySessionID(tc.sessionPath, tc.workspace)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("gatewaySessionID = %q, want empty", got)
				}
				return
			}
			if len(got) > 30 {
				t.Fatalf("gatewaySessionID = %q, want <= 30 chars (gateway truncates)", got)
			}
			if got[:3] != "rx-" {
				t.Fatalf("gatewaySessionID = %q, want rx- prefix", got)
			}
			// The project slug must be the visible tail: the opencode
			// dashboard shows the last 8 chars of the stored value, so the
			// final word of the slug must land in that window.
			if !strings.HasSuffix(got, "-"+tc.wantSlug) {
				t.Fatalf("gatewaySessionID = %q, want suffix -%s", got, tc.wantSlug)
			}
			lastWord := tc.wantSlug
			if i := strings.LastIndexByte(lastWord, '-'); i >= 0 {
				lastWord = lastWord[i+1:]
			}
			if tail := got[len(got)-8:]; !strings.Contains(tail, lastWord) {
				t.Fatalf("gatewaySessionID tail = %q, want it to carry %q", tail, lastWord)
			}
		})
	}
}

func TestSessionTimestamp(t *testing.T) {
	got := sessionTimestamp("/x/20260809-000220.677640881-deepseek-v4-flash.jsonl")
	if got != "08090002" {
		t.Fatalf("sessionTimestamp = %q, want 08090002", got)
	}
	if v := sessionTimestamp("plain-name.jsonl"); v != "" {
		t.Fatalf("sessionTimestamp(plain) = %q, want empty", v)
	}
	if v := sessionTimestamp(""); v != "" {
		t.Fatalf("sessionTimestamp(empty) = %q, want empty", v)
	}
}

func TestProjectSlug(t *testing.T) {
	cases := map[string]string{
		"/home/edugoat/Dev/DeepSeek-Reasonix": "dee-rea",
		"DeepSeek-Reasonix":                   "dee-rea",
		"segurofeliz":                         "seg",
		"/x/whatsapp-audio-transcriber":       "wha-aud-tra",
		"":                                    "",
		"/":                                   "",
	}
	for in, want := range cases {
		if got := projectSlug(in); got != want {
			t.Fatalf("projectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
