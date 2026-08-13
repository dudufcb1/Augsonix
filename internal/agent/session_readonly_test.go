package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// recordingBash stands in for the real bash tool: writer-capable, and it
// records whether the command reached execution.
type recordingBash struct{ ran bool }

func (*recordingBash) Name() string            { return "bash" }
func (*recordingBash) Description() string     { return "fake bash" }
func (*recordingBash) ReadOnly() bool          { return false }
func (*recordingBash) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b *recordingBash) Execute(context.Context, json.RawMessage) (string, error) {
	b.ran = true
	return "ran", nil
}

// TestSessionReadOnlyAdmissionLeavesNoWriterBehind pins what the bound buys:
// the registry stops being writer-capable at all, which is also what keeps the
// delivery mutation expectation from arming for the session.
func TestSessionReadOnlyAdmissionLeavesNoWriterBehind(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	reg.Add(&recordingBash{})

	dropped := reg.SetAdmission(SessionReadOnlyAdmission())
	if len(dropped) != 1 || dropped[0] != "fake_write" {
		t.Fatalf("dropped = %v, want [fake_write]", dropped)
	}
	if registryHasWriterTools(reg) {
		t.Fatalf("registry still counts as writer-capable: %v", reg.Names())
	}
}

// TestSessionReadOnlyAdmissionKeepsBashReadOnly covers the substitution: bash
// stays available for inspection but a mutating command never reaches the
// shell, so the promise does not rest on the model's good behavior.
func TestSessionReadOnlyAdmissionKeepsBashReadOnly(t *testing.T) {
	inner := &recordingBash{}
	reg := tool.NewRegistry()
	reg.Add(inner)
	reg.SetAdmission(SessionReadOnlyAdmission())

	bash, ok := reg.Get("bash")
	if !ok {
		t.Fatal("bash was dropped instead of wrapped")
	}
	if _, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf build"}`)); err == nil {
		t.Fatal("a destructive command was accepted")
	}
	if inner.ran {
		t.Fatal("a destructive command reached the shell")
	}
	if _, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"ls"}`)); err != nil {
		t.Fatalf("read-only command was blocked: %v", err)
	}
	if !inner.ran {
		t.Fatal("read-only command never reached the shell")
	}
}

// slippedWriter is a writer-capable tool that records whether it ran.
type slippedWriter struct{ ran bool }

func (*slippedWriter) Name() string            { return "slipped_writer" }
func (*slippedWriter) Description() string     { return "fake writer" }
func (*slippedWriter) ReadOnly() bool          { return false }
func (*slippedWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w *slippedWriter) Execute(context.Context, json.RawMessage) (string, error) {
	w.ran = true
	return "wrote", nil
}

// TestReadOnlyExecutionBlocksAWriterThatSlippedIn covers the second layer: even
// with a writer present — which is how a use_capability dispatch reaches a tool
// the registry filter never saw — a read-only session must refuse to run it.
func TestReadOnlyExecutionBlocksAWriterThatSlippedIn(t *testing.T) {
	writer := &slippedWriter{}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(writer)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("w1", "slipped_writer", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "cannot write here"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession("sys"), Options{ReadOnlyExecution: true}, event.Discard)
	if err := a.Run(context.Background(), "read the file and report"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writer.ran {
		t.Fatal("a writer ran inside a read-only session")
	}
}
