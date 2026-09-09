package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func (c *client) send(ctx context.Context, body map[string]any, sessionID string) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("responses: marshal request: %w", err)
	}
	newRequest := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.requestURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for name, value := range c.extraHeaders {
			req.Header.Set(name, value)
		}
		req.Header.Set("Content-Type", "application/json")
		bearer := c.apiKey
		if c.tokens != nil {
			// Resolved per attempt, not per stream: a retry after a long tool
			// turn must not reuse a bearer that expired while it ran.
			token, err := c.tokens(ctx)
			if err != nil {
				return nil, err
			}
			bearer = token.Token
			for name, value := range token.Headers {
				req.Header.Set(name, value)
			}
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		// La pasarela agrupa por sesión lógica; el cliente openai ya la manda y
		// sin esto los perfiles de tipo responses llegaban sin agrupar.
		openai.ApplyOpenCodeSessionHeader(req.Header, c.requestURL, sessionID)
		if c.caps.sessionCacheHeader && c.sessionCache {
			req.Header.Set("x-dashscope-session-cache", "enable")
		}
		return req, nil
	}
	return provider.SendWithRetry(ctx, c.http, c.sendOpts(), newRequest)
}
