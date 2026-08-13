package chatgptauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"time"
)

// callbackPort está registrado en OpenAI contra ClientID. Un puerto distinto
// hace que la autorización sea rechazada, así que no se sortea uno libre.
const callbackPort = 1455

const callbackPath = "/auth/callback"

// LoginTimeout acota cuánto se espera a que el usuario termine en el navegador.
const LoginTimeout = 5 * time.Minute

// Login abre el navegador contra auth.openai.com, espera el redirect al puerto
// 1455 y guarda la sesión. openURL recibe la URL a abrir; devolver un error
// solo aborta si el usuario no puede abrirla por su cuenta.
func Login(ctx context.Context, store Store, client *http.Client, openURL func(string) error) (Tokens, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	verifier, err := randomURLSafe(43)
	if err != nil {
		return Tokens{}, err
	}
	state, err := randomURLSafe(32)
	if err != nil {
		return Tokens{}, err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return Tokens{}, fmt.Errorf("chatgptauth: el puerto %d está ocupado y OpenAI exige justo ese para el retorno del login: %w", callbackPort, err)
	}
	defer listener.Close()

	results := make(chan callbackResult, 1)
	server := &http.Server{Handler: callbackHandler(state, results), ReadHeaderTimeout: 5 * time.Second}
	serveDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()
	defer server.Close()

	redirect := fmt.Sprintf("http://localhost:%d%s", callbackPort, callbackPath)
	if err := openURL(authorizeURL(redirect, verifier, state)); err != nil {
		return Tokens{}, fmt.Errorf("chatgptauth: abrir la página de autorización: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, LoginTimeout)
	defer cancel()
	var callback callbackResult
	select {
	case <-waitCtx.Done():
		return Tokens{}, fmt.Errorf("chatgptauth: la autorización no se completó a tiempo")
	case err := <-serveDone:
		if err != nil {
			return Tokens{}, fmt.Errorf("chatgptauth: el servidor de retorno falló: %w", err)
		}
		return Tokens{}, fmt.Errorf("chatgptauth: el servidor de retorno se detuvo antes de recibir la autorización")
	case callback = <-results:
	}
	if callback.err != nil {
		return Tokens{}, callback.err
	}
	response, err := postForm(ctx, client, Issuer+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {callback.code},
		"redirect_uri":  {redirect},
		"client_id":     {ClientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("chatgptauth: canjear el código de autorización: %w", err)
	}
	tokens := mergeTokens(Tokens{}, response)
	if err := store.Save(tokens); err != nil {
		return tokens, err
	}
	return tokens, nil
}

type callbackResult struct {
	code string
	err  error
}

// callbackHandler acepta un solo retorno: valida el state contra CSRF y
// responde una página mínima para que el usuario cierre la pestaña.
func callbackHandler(state string, results chan<- callbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if reason := firstNonEmpty(query.Get("error_description"), query.Get("error")); reason != "" {
			deliver(results, callbackResult{err: fmt.Errorf("chatgptauth: OpenAI rechazó la autorización: %s", reason)})
			writeCallbackPage(w, http.StatusBadRequest, "No se pudo completar la autorización.")
			return
		}
		code := query.Get("code")
		if code == "" {
			deliver(results, callbackResult{err: fmt.Errorf("chatgptauth: el retorno no traía código de autorización")})
			writeCallbackPage(w, http.StatusBadRequest, "No se pudo completar la autorización.")
			return
		}
		if query.Get("state") != state {
			deliver(results, callbackResult{err: fmt.Errorf("chatgptauth: el state del retorno no coincide; se descarta por seguridad")})
			writeCallbackPage(w, http.StatusBadRequest, "No se pudo completar la autorización.")
			return
		}
		deliver(results, callbackResult{code: code})
		writeCallbackPage(w, http.StatusOK, "Listo. Ya puedes cerrar esta pestaña y volver a la terminal.")
	})
	return mux
}

func deliver(results chan<- callbackResult, result callbackResult) {
	select {
	case results <- result:
	default:
	}
}

func writeCallbackPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>Reasonix</title>"+
		"<body style=\"font:16px system-ui;padding:3rem\"><p>%s</p></body>", message)
}

// authorizeURL arma la URL de autorización. Los dos parámetros que no son
// estándar (organizaciones en el id_token y el flujo simplificado) son los que
// hacen que el token sirva contra el backend de Codex.
func authorizeURL(redirect, verifier, state string) string {
	challenge := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {ClientID},
		"redirect_uri":               {redirect},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
		"originator":                 {"reasonix"},
	}
	return Issuer + "/oauth/authorize?" + params.Encode()
}

// randomURLSafe genera un valor aleatorio con el alfabeto que admite PKCE.
func randomURLSafe(length int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("chatgptauth: generar el desafío PKCE: %w", err)
	}
	out := make([]byte, length)
	for i, b := range raw {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
