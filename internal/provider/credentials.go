package provider

import "context"

// TokenSourceKey is the Config.Extra key under which an OAuth-backed provider
// receives its resolver. Absent for every static-API-key provider, whose
// request path stays byte-identical.
const TokenSourceKey = "token_source"

// BearerToken is one resolved short-lived credential: the bearer itself plus
// any headers that identify the account it belongs to.
type BearerToken struct {
	Token   string
	Headers map[string]string
}

// TokenSource yields the currently valid bearer, refreshing it when needed.
// Implementations must be safe for concurrent use by one provider instance.
type TokenSource func(ctx context.Context) (BearerToken, error)
