package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sessiontemp"
)

func TestTruncateToolOutputSpillWritesFullFile(t *testing.T) {
	m := sessiontemp.NewWithRoot(t.TempDir())
	m.Retain()
	defer m.Release()

	original := "line\n" + strings.Repeat("payload-", maxToolOutputBytes/2) + "\nend"
	body, notice := truncateToolOutputSpill(context.Background(), m, original, "n8n", "call_1")
	if notice == "" {
		t.Fatal("spill must report a truncation notice")
	}
	if len(body) > maxToolOutputBytes {
		t.Fatalf("bounded body oversized: %d > %d", len(body), maxToolOutputBytes)
	}
	if !strings.HasPrefix(body, "tool output spilled to ") {
		t.Fatalf("body must lead with the spill path, got %.120q", body)
	}
	path := strings.Fields(body)[4]
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	if string(got) != original {
		t.Fatalf("spill file content mismatch: got %d bytes, want %d", len(got), len(original))
	}
	if !strings.Contains(body, "grep -n") || !strings.Contains(body, "read_file") {
		t.Error("spill preview must tell the model how to read the file")
	}
}

func TestTruncateToolOutputSpillFallsBackWithoutManager(t *testing.T) {
	original := strings.Repeat("x", maxToolOutputBytes+1000)
	body, notice := truncateToolOutputSpill(context.Background(), nil, original, "read_file", "call-1")
	if notice == "" {
		t.Fatal("over-cap output without a manager must still truncate")
	}
	if strings.Contains(body, "spilled to") {
		t.Fatalf("no manager must fall back to classic truncation, got %.200q", body)
	}
	if !strings.Contains(body, "truncated tool=read_file call_id=call-1") {
		t.Fatalf("fallback marker missing tool identity: %.200q", body)
	}
	if len(body) > maxToolOutputBytes {
		t.Fatalf("fallback body oversized: %d", len(body))
	}
}

func TestTruncateToolOutputSpillReadsManagerFromContext(t *testing.T) {
	m := sessiontemp.NewWithRoot(t.TempDir())
	m.Retain()
	defer m.Release()
	ctx := sessiontemp.WithManager(context.Background(), m)

	body, notice := truncateToolOutputSpill(ctx, nil, strings.Repeat("y", maxToolOutputBytes+1), "bash", "call-2")
	if notice == "" || !strings.Contains(body, "spilled to") {
		t.Fatalf("subagent context manager must enable the spill: notice=%q body=%.200q", notice, body)
	}
	path := strings.Fields(body)[4]
	if !strings.HasPrefix(path, m.Dir()) {
		t.Fatalf("spill path %q outside session temp dir %q", path, m.Dir())
	}
}

func TestSpillFileNameSanitizes(t *testing.T) {
	cases := map[string]string{
		"call_123":               "tool_output_call_123.txt",
		"../evil:name?":          "tool_output____evil_name_.txt",
		"":                       "tool_output_tool.txt", // empty id falls back to the tool name
		strings.Repeat("n", 100): "",
	}
	for in, want := range cases {
		got := spillFileName("tool", in)
		if want == "" {
			if len(got) <= len("tool_output_")+len(".txt") {
				t.Errorf("spillFileName(%q) = %q, want a bounded name", in, got)
			}
			continue
		}
		if got != want {
			t.Errorf("spillFileName(%q) = %q, want %q", in, got, want)
		}
		if filepath.Base(got) != got || strings.ContainsAny(got, "/\\") {
			t.Errorf("spillFileName(%q) = %q escapes the session dir", in, got)
		}
	}
}

func TestTruncateToolOutputSpillPreviewFitsCapEvenForHugeOutput(t *testing.T) {
	m := sessiontemp.NewWithRoot(t.TempDir())
	m.Retain()
	defer m.Release()

	big := strings.Repeat("z", 4<<20) // 4 MiB
	body, notice := truncateToolOutputSpill(context.Background(), m, big, "bash", "call_9")
	if notice == "" {
		t.Fatal("4 MiB output must spill")
	}
	if len(body) > maxToolOutputBytes {
		t.Fatalf("body oversized for huge input: %d", len(body))
	}
	if !strings.Contains(body, "spilled to") {
		t.Fatalf("huge output must use the spill path: %.160q", body)
	}
}
