package openai

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
		Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")),
		Request:    req,
	}, nil
}

func TestStreamSendsOpenCodeSessionHeader(t *testing.T) {
	rt := &captureRoundTripper{}
	p, err := New(provider.Config{
		Name:    "opencode-go",
		BaseURL: "https://opencode.ai/zen/go/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "real-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cl := p.(*client)
	cl.http = &http.Client{Transport: rt}

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
	p, err := New(provider.Config{
		Name:    "plain",
		BaseURL: "https://api.example.com/v1",
		Model:   "model-a",
		APIKey:  "real-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cl := p.(*client)
	cl.http = &http.Client{Transport: rt}

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

func TestIsOpenCode(t *testing.T) {
	cases := map[string]bool{
		"https://opencode.ai/zen/go/v1": true,
		"https://opencode.ai/zen/v1":    true,
		"https://api.opencode.ai/zen":   true,
		"https://opencode.ai":           true,
		"https://api.deepseek.com":      false,
		"https://api.openai.com/v1":     false,
		"https://opencode.example.com":  false,
		"not a url":                     false,
		"":                              false,
	}
	for in, want := range cases {
		if got := IsOpenCode(in); got != want {
			t.Fatalf("IsOpenCode(%q) = %v, want %v", in, got, want)
		}
	}
}
