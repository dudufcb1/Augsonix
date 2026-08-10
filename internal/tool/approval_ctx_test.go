package tool

import (
	"context"
	"testing"
)

func TestApprovalModeRoundtrip(t *testing.T) {
	ctx := context.Background()
	if got := ApprovalModeFrom(ctx); got != "" {
		t.Fatalf("empty ctx should yield empty mode, got %q", got)
	}
	ctx = WithApprovalMode(ctx, ApprovalModeYolo)
	if got := ApprovalModeFrom(ctx); got != ApprovalModeYolo {
		t.Fatalf("WithApprovalMode roundtrip failed, got %q", got)
	}
}

func TestWithApprovalModeEmptyIsNoop(t *testing.T) {
	ctx := context.Background()
	after := WithApprovalMode(ctx, "")
	if ApprovalModeFrom(after) != "" {
		t.Error("empty mode should not attach a value")
	}
}
