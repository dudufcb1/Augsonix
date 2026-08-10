package anthropic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// captureRoundTripper records the outgoing request and replies with a canned
// SSE stream, so header behavior can be asserted without real network access.
type captureRoundTripper struct {
	got *http.Request
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.got = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseFixture)),
		Request:    req,
	}, nil
}

func TestStreamSendsOpenCodeSessionHeader(t *testing.T) {
	rt := &captureRoundTripper{}
	cl := &client{
		name:    "opencode-zen-anthropic",
		baseURL: "https://opencode.ai/zen",
		model:   "claude-sonnet-4-6",
		apiKey:  "real-key",
		http:    &http.Client{Transport: rt},
	}
	ch, err := cl.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		SessionID: "rx-08090022-dee-rea",
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if rt.got == nil {
		t.Fatal("no request captured")
	}
	if got := rt.got.Header.Get("x-opencode-session"); got != "rx-08090022-dee-rea" {
		t.Fatalf("x-opencode-session = %q, want the request SessionID", got)
	}
}

func TestStreamOmitsOpenCodeSessionHeaderForOtherHosts(t *testing.T) {
	rt := &captureRoundTripper{}
	cl := &client{
		name:    "plain",
		baseURL: "https://api.anthropic.com",
		model:   "claude-sonnet-4-6",
		apiKey:  "real-key",
		http:    &http.Client{Transport: rt},
	}
	ch, err := cl.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		SessionID: "rx-08090022-dee-rea",
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := rt.got.Header.Get("x-opencode-session"); got != "" {
		t.Fatalf("x-opencode-session = %q on non-gateway host, want omitted", got)
	}
}
