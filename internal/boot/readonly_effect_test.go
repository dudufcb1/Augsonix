package boot

// Effect test for the session read-only bound: it asserts the file system
// after a real Build, not that a helper returned the right list.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// readOnlyProbeProvider asks for a file write on its first round and then
// answers, so a test can assert on disk what the session was able to do.
type readOnlyProbeProvider struct {
	mu     sync.Mutex
	rounds int
	path   string
}

func (p *readOnlyProbeProvider) Name() string { return "boot-readonly-probe" }

func (p *readOnlyProbeProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if round == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        "w1",
			Name:      "write_file",
			Arguments: fmt.Sprintf(`{"path":%q,"content":"written"}`, p.path),
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// effectReadOnlyRun builds the real stack with Options.ReadOnly set as asked,
// lets the model attempt one write, and reports whether the file appeared.
func effectReadOnlyRun(t *testing.T, kind string, readOnly bool) bool {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	target := filepath.Join(dir, "probe.txt")
	rec := &readOnlyProbeProvider{path: target}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", "default_model = \"test-model\"\n\n"+
		"[agent]\nsystem_prompt = \"BASE\"\n\n"+
		"[[providers]]\nname = \"test-model\"\nkind = \""+kind+"\"\nmodel = \"x\"\n")

	ctrl, err := Build(context.Background(), Options{
		Sink:                 event.Discard,
		ReadOnly:             readOnly,
		HeadlessApprovalMode: control.ToolApprovalYolo,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	// The turn's own outcome is beside the point (the control arm ends on a
	// readiness complaint for writing without verifying); what is measured is
	// whether the write reached the disk.
	_ = ctrl.Run(context.Background(), "write the probe file")
	_, statErr := os.Stat(target)
	return statErr == nil
}

// TestEffectReadOnlySessionCannotWriteThroughRealBuild pins --read-only at the
// only boundary that matters: the file must not appear, with tool approval on
// yolo so no permission rule is doing the work. The control arm proves the
// probe writes when the bound is off, so the read-only arm passing means the
// bound held and not that the provider failed to try.
func TestEffectReadOnlySessionCannotWriteThroughRealBuild(t *testing.T) {
	if !effectReadOnlyRun(t, "boot-readonly-off", false) {
		t.Fatal("control arm never wrote the probe file; the test cannot detect a regression")
	}
	if effectReadOnlyRun(t, "boot-readonly-on", true) {
		t.Fatal("read-only session wrote a file")
	}
}
