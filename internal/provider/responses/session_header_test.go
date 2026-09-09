package responses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// captureTransport guarda la petición saliente sin salir a la red. Responde
// 200 en vez de error para que el reintento no repita el envío: con un fallo,
// cada caso tardaba minuto y medio en agotar la política de reintentos.
type captureTransport struct{ got *http.Request }

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.got = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// sendFor arma la petición como lo haría un envío real y devuelve sus cabeceras.
func sendFor(t *testing.T, requestURL, sessionID string) http.Header {
	t.Helper()
	rt := &captureTransport{}
	c := &client{
		model:      "muse-spark",
		apiKey:     "real-key",
		requestURL: requestURL,
		http:       &http.Client{Transport: rt},
	}
	_, _ = c.send(context.Background(), map[string]any{"model": "muse-spark"}, sessionID)
	if rt.got == nil {
		t.Fatal("no se capturó ninguna petición")
	}
	return rt.got.Header
}

func TestSendCarriesTheOpenCodeSessionHeader(t *testing.T) {
	// Los perfiles de tipo responses que apuntan a la pasarela salían sin la
	// cabecera de sesión: solo la mandaban los clientes openai y anthropic, así
	// que sus peticiones llegaban sin poder agruparse por conversación.
	t.Parallel()
	h := sendFor(t, "https://opencode.ai/zen/go/v1/responses", "rx-08090022-dee-rea")
	if got := h.Get("x-opencode-session"); got != "rx-08090022-dee-rea" {
		t.Fatalf("x-opencode-session = %q, se esperaba el id de la sesión", got)
	}
}

func TestSendOmitsTheSessionHeaderForOtherHosts(t *testing.T) {
	// La cabecera es de esa pasarela; mandarla a otro proveedor filtraría un
	// identificador nuestro a quien no lo pidió.
	t.Parallel()
	h := sendFor(t, "https://chatgpt.com/backend-api/codex/responses", "rx-08090022-dee-rea")
	if got := h.Get("x-opencode-session"); got != "" {
		t.Fatalf("se filtró la cabecera a otro host: %q", got)
	}
}

func TestSendOmitsTheSessionHeaderWhenThereIsNoSession(t *testing.T) {
	// Sin id no hay nada que agrupar, y una cabecera vacía solo confunde.
	t.Parallel()
	h := sendFor(t, "https://opencode.ai/zen/go/v1/responses", "")
	if _, ok := h["X-Opencode-Session"]; ok {
		t.Fatalf("se mandó la cabecera vacía: %v", h)
	}
}
