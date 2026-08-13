package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// readOnlyErrandRun replays a review errand: one read, then the report. The
// registry is writer-capable because that is what a plain top-level session
// builds — the errand's read-only nature comes from its instruction, not from
// a filtered tool set.
func readOnlyErrandRun(t *testing.T, task string) error {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	read := []provider.Chunk{
		toolCallChunk("c1", "read_file", `{"path":"a.go"}`),
		{Type: provider.ChunkDone},
	}
	report := []provider.Chunk{
		{Type: provider.ChunkText, Text: `{"findings": []}`},
		{Type: provider.ChunkDone},
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{read, report, report, report}}
	a := New(prov, reg, NewSession("sys"), Options{DeliveryProfile: true}, event.Discard)
	return a.Run(context.Background(), task)
}

// TestDeliveryInstructedReadOnlyErrandNeedsNoMutation covers the reviewer case:
// a prompt the classifier reads as a mutation (its rules name fixes and
// patches) whose instruction forbids changing anything. Holding it to a write
// receipt deadlocks it — the errand can only ever read. The second arm is the
// same errand without the read-only clause, and proves the gate still fires.
func TestDeliveryInstructedReadOnlyErrandNeedsNoMutation(t *testing.T) {
	readOnly := "review the staged diff and report which rules it breaks: " +
		"call sites, a renamed field, a missing patch. read only, do not modify any files."
	if err := readOnlyErrandRun(t, readOnly); err != nil {
		t.Fatalf("read-only errand was held to a mutation it may not perform: %v", err)
	}

	mutating := "fix the staged diff: repair the call sites and rename the field"
	err := readOnlyErrandRun(t, mutating)
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("a real mutation request escaped the gate: %v", err)
	}
	if !strings.Contains(readinessErr.Reason, "state change") {
		t.Fatalf("readiness reason = %q, want missing state change", readinessErr.Reason)
	}
}

// TestDeliverySpanishReadOnlyErrandNeedsNoMutation is the same contract for an
// errand written in Spanish: the constraint vocabulary must not be
// English-only, or every non-English read-only task keeps deadlocking.
func TestDeliverySpanishReadOnlyErrandNeedsNoMutation(t *testing.T) {
	task := "revisa el diff staged y reporta que reglas rompe: call sites, " +
		"un campo renombrado, un patch que falta. es de solo lectura."
	if err := readOnlyErrandRun(t, task); err != nil {
		t.Fatalf("Spanish read-only errand was held to a mutation receipt: %v", err)
	}
}
