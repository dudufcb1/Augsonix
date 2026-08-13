package boot

import (
	"context"
	"fmt"

	"reasonix/internal/chatgptauth"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// NewProvider builds a provider.Provider from a configured entry. Exported so
// custom assemblers (e.g. the ACP per-session factory) can reuse it without
// going through the full Build.
func NewProvider(e *config.ProviderEntry) (provider.Provider, error) {
	return NewProviderWithProxy(e, netclient.ProxySpec{Mode: netclient.ModeAuto})
}

// NewProviderWithProxy builds a provider.Provider with the configured ordinary
// network proxy settings.
func NewProviderWithProxy(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.Provider, error) {
	tokens, err := providerTokenSource(e, proxy)
	if err != nil {
		return nil, err
	}
	return provider.New(e.Kind, provider.Config{
		Name:    e.Name,
		BaseURL: e.BaseURL,
		Model:   e.Model,
		APIKey:  e.APIKey(),
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			provider.TokenSourceKey: tokens,
			"api_key_env":           e.APIKeyEnv,
			"api_key_source":        e.APIKeySourceLabel(),
			"thinking":              e.Thinking,
			"effort":                config.EffectiveEffort(e),
			"supported_efforts":     e.SupportedEfforts,
			"reasoning_protocol":    config.ReasoningProtocolForEntry(e),
			"max_output_tokens":     e.MaxOutputTokens,
			"chat_url":              e.ChatURL,
			"request_url":           e.RequestURL,
			"headers":               e.Headers,
			"extra_body":            e.ExtraBody,
			"auth_header":           e.AuthHeader,
			"proxy_spec":            proxy,
			"vision":                config.EffectiveVision(e),
			"vision_detail":         e.VisionDetail,
			"web_search":            config.EffectiveWebSearch(e),
			"mode":                  e.ResponsesMode,
			// Keep nil as nil so the responses provider can vendor-detect its
			// default instead of accidentally treating every endpoint as stateful.
			"stateful": e.ResponsesStateful,
		},
	})
}

// providerTokenSource builds the credential resolver for entries whose auth is
// not a static API key. Returns nil for every other entry, which keeps their
// request path byte-identical to before this hook existed.
func providerTokenSource(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.TokenSource, error) {
	if e == nil || !e.UsesChatGPTSession() {
		return nil, nil
	}
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", e.Name, err)
	}
	source := chatgptauth.NewSource(chatgptauth.Store{
		Path:     config.ChatGPTAuthPath(),
		Fallback: config.CodexCLIAuthPath(),
	}, client)
	return func(ctx context.Context) (provider.BearerToken, error) {
		credential, err := source.Credential(ctx)
		if err != nil {
			return provider.BearerToken{}, err
		}
		headers := map[string]string{"originator": "reasonix"}
		if credential.AccountID != "" {
			headers["ChatGPT-Account-Id"] = credential.AccountID
		}
		return provider.BearerToken{Token: credential.AccessToken, Headers: headers}, nil
	}, nil
}
