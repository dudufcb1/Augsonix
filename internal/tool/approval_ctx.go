package tool

import "context"

// approvalModeCtxKey is unexported so only this package can read the value.
type approvalModeCtxKey struct{}

// WithApprovalMode attaches the session tool-approval posture (ask|auto|yolo)
// to ctx so the write tools can relax confinement for full-access sessions.
// The empty mode is left untouched (no value attached).
func WithApprovalMode(ctx context.Context, mode string) context.Context {
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, approvalModeCtxKey{}, mode)
}

// ApprovalModeFrom returns the posture attached by WithApprovalMode ("" when
// absent).
func ApprovalModeFrom(ctx context.Context) string {
	if v, ok := ctx.Value(approvalModeCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ApprovalModeYolo is the full-access tool-approval posture. It mirrors
// control.ToolApprovalYolo so the write confinement can compare modes without
// importing the control package (which would create a cycle).
const ApprovalModeYolo = "yolo"
