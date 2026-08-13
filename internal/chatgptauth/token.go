package chatgptauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// ClientID es el cliente público del Codex CLI. OpenAI tiene registrado
	// contra él el redirect del puerto 1455; no es configurable.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// Issuer emite y refresca los tokens.
	Issuer = "https://auth.openai.com"
	// Endpoint es el backend de Codex que atiende la suscripción.
	Endpoint = "https://chatgpt.com/backend-api/codex/responses"

	// refreshMargin es cuánta vida restante basta para reusar el token. Un
	// turno largo con herramientas puede pasar minutos entre requests.
	refreshMargin = 5 * time.Minute
	// maxTokenBody acota la respuesta del issuer.
	maxTokenBody = 1 << 20
)

// Source entrega un bearer vigente. Una sola instancia se comparte entre los
// requests de una sesión: el refresco corre una vez aunque diez llamadas lo
// pidan a la vez.
type Source struct {
	store  Store
	client *http.Client
	issuer string

	mu      sync.Mutex
	tokens  Tokens
	loaded  bool
	pending chan struct{}
}

// Credential es lo que el provider necesita para armar una request.
type Credential struct {
	AccessToken string
	AccountID   string
}

// NewSource construye la fuente sobre un almacén. El cliente HTTP se recibe de
// fuera para heredar el proxy configurado.
func NewSource(store Store, client *http.Client) *Source {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Source{store: store, client: client, issuer: Issuer}
}

// Credential devuelve el token de acceso vigente, refrescándolo si le quedan
// menos de cinco minutos. El error ya es legible para el usuario final.
func (s *Source) Credential(ctx context.Context) (Credential, error) {
	s.mu.Lock()
	if !s.loaded {
		tokens, err := s.store.Load()
		if err != nil {
			s.mu.Unlock()
			return Credential{}, err
		}
		s.tokens, s.loaded = tokens, true
	}
	current := s.tokens
	if usable(current) {
		s.mu.Unlock()
		return Credential{AccessToken: current.AccessToken, AccountID: current.AccountID}, nil
	}
	if s.pending != nil {
		wait := s.pending
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-wait:
		}
		return s.current()
	}
	if strings.TrimSpace(current.RefreshToken) == "" {
		s.mu.Unlock()
		return Credential{}, ErrNoCredentials
	}
	done := make(chan struct{})
	s.pending = done
	s.mu.Unlock()

	refreshed, err := s.refresh(ctx, current)
	s.mu.Lock()
	s.pending = nil
	if err == nil {
		s.tokens = refreshed
	}
	s.mu.Unlock()
	close(done)
	if err != nil {
		return Credential{}, err
	}
	return Credential{AccessToken: refreshed.AccessToken, AccountID: refreshed.AccountID}, nil
}

func (s *Source) current() (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !usable(s.tokens) {
		return Credential{}, fmt.Errorf("chatgptauth: el refresco de la sesión no dejó un token utilizable")
	}
	return Credential{AccessToken: s.tokens.AccessToken, AccountID: s.tokens.AccountID}, nil
}

func (s *Source) refresh(ctx context.Context, current Tokens) (Tokens, error) {
	response, err := postForm(ctx, s.client, s.issuer+"/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.RefreshToken},
		"client_id":     {ClientID},
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("chatgptauth: refrescar la sesión de ChatGPT: %w", err)
	}
	next := mergeTokens(current, response)
	if err := s.store.Save(next); err != nil {
		return next, err
	}
	return next, nil
}

// tokenResponse es la respuesta del issuer en los tres flujos (código,
// dispositivo y refresco).
type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// mergeTokens conserva lo que la respuesta omite: el issuer puede no reemitir
// el refresh token, y perderlo obligaría a volver a hacer login.
func mergeTokens(current Tokens, response tokenResponse) Tokens {
	next := Tokens{
		IDToken:      firstNonEmpty(response.IDToken, current.IDToken),
		AccessToken:  firstNonEmpty(response.AccessToken, current.AccessToken),
		RefreshToken: firstNonEmpty(response.RefreshToken, current.RefreshToken),
		LastRefresh:  time.Now().UTC(),
	}
	next.AccountID = firstNonEmpty(AccountID(next), current.AccountID)
	return next
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody))
	if err != nil {
		return tokenResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("el servidor respondió %d: %s", resp.StatusCode, snippet(body))
	}
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return tokenResponse{}, fmt.Errorf("respuesta ilegible del servidor: %w", err)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return tokenResponse{}, fmt.Errorf("el servidor no devolvió un token de acceso")
	}
	return parsed, nil
}

// usable reporta si el token sirve para el próximo request sin refrescarlo. Un
// vencimiento ilegible cuenta como vencido: mejor refrescar de más.
func usable(tokens Tokens) bool {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return false
	}
	expiry := ExpiresAt(tokens.AccessToken)
	if expiry.IsZero() {
		return false
	}
	return time.Now().Add(refreshMargin).Before(expiry)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func snippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	return text
}
