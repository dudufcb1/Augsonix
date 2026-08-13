package responses

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func conversationWithReasoning() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "hola"},
		{
			Role: provider.RoleAssistant, Content: "voy a buscar", ReasoningContent: "pensando",
			ReasoningID: "rs_1", ReasoningStatus: "completed",
			ResponsesItems: []json.RawMessage{
				json.RawMessage(`{"type":"reasoning","id":"rs_1","encrypted_content":"OPAQUE","summary":[]}`),
			},
			ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: "{}"}},
		},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "bash", Content: "ok"},
	}
}

// El backend de Codex no guarda la respuesta, así que la request debe pedir
// explícitamente que no se guarde y que el razonamiento vuelva cifrado: sin
// esas dos claves el siguiente turno pierde la cadena de pensamiento.
func TestChatGPTRequestDisablesStoreAndAsksForEncryptedReasoning(t *testing.T) {
	client := New(Config{
		Name: "chatgpt-codex", Model: "gpt-5.5",
		BaseURL: "https://chatgpt.com/backend-api/codex",
	}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: conversationWithReasoning()})

	if store, ok := body["store"].(bool); !ok || store {
		t.Errorf("store = %#v, esperaba false", body["store"])
	}
	include, _ := body["include"].([]string)
	if !slices.Contains(include, "reasoning.encrypted_content") {
		t.Errorf("include = %#v, esperaba reasoning.encrypted_content", body["include"])
	}
	// El Codex CLI no manda techo de salida: la suscripción aplica el suyo.
	if _, present := body["max_output_tokens"]; present {
		t.Errorf("max_output_tokens = %#v, esperaba que se omitiera", body["max_output_tokens"])
	}
}

// El item de razonamiento se reenvía tal como lo emitió el servidor. Uno
// reconstruido a partir del texto no trae encrypted_content y la API lo rechaza.
func TestChatGPTReplaysTheServerReasoningItemVerbatim(t *testing.T) {
	client := New(Config{
		Name: "chatgpt-codex", Model: "gpt-5.5",
		BaseURL: "https://chatgpt.com/backend-api/codex",
	}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: conversationWithReasoning()})

	input, ok := body["input"].([]map[string]any)
	if !ok {
		t.Fatalf("input = %T, esperaba la lista de items", body["input"])
	}
	var reasoningItems []map[string]any
	for _, item := range input {
		if item["type"] == "reasoning" {
			reasoningItems = append(reasoningItems, item)
		}
	}
	if len(reasoningItems) != 1 {
		t.Fatalf("items de reasoning = %d, esperaba exactamente el del servidor", len(reasoningItems))
	}
	if got := reasoningItems[0]["encrypted_content"]; got != "OPAQUE" {
		t.Errorf("encrypted_content = %#v, esperaba el emitido por el servidor", got)
	}
	if _, rebuilt := reasoningItems[0]["content"]; rebuilt {
		t.Error("el item reconstruido viajó junto al del servidor; la API lo rechazaría")
	}
}

// Un reasoning sin estado cifrado no se puede replicar con store:false. Debe
// caer, no viajar a medias y provocar un 400 en mitad de la conversación.
func TestChatGPTDropsReasoningWithoutEncryptedState(t *testing.T) {
	client := New(Config{
		Name: "chatgpt-codex", Model: "gpt-5.5",
		BaseURL: "https://chatgpt.com/backend-api/codex",
	}).(*client)
	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "hola"},
		{
			Role: provider.RoleAssistant, Content: "listo", ReasoningContent: "pensando",
			ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"rs_2"}`)},
		},
	}
	body, _, _ := client.buildRequestBody(provider.Request{Messages: messages})
	for _, item := range body["input"].([]map[string]any) {
		if item["type"] == "reasoning" {
			t.Fatalf("se replicó un reasoning sin encrypted_content: %#v", item)
		}
	}
}

// La sesión OAuth se resuelve por request y sus headers identifican la cuenta.
// Sin ChatGPT-Account-Id el backend no sabe a qué suscripción cargar el uso.
func TestChatGPTRequestCarriesTheResolvedSessionHeaders(t *testing.T) {
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		Name: "chatgpt-codex", Model: "gpt-5.5",
		BaseURL: "https://chatgpt.com/backend-api/codex", RequestURL: server.URL,
		Tokens: func(context.Context) (provider.BearerToken, error) {
			return provider.BearerToken{
				Token:   "tok-vigente",
				Headers: map[string]string{"ChatGPT-Account-Id": "acct-9", "originator": "reasonix"},
			}, nil
		},
	}).(*client)
	if _, err := client.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hola"}},
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	headers := <-seen
	if got := headers.Get("Authorization"); got != "Bearer tok-vigente" {
		t.Errorf("Authorization = %q, esperaba el bearer resuelto", got)
	}
	if got := headers.Get("ChatGPT-Account-Id"); got != "acct-9" {
		t.Errorf("ChatGPT-Account-Id = %q, esperaba acct-9", got)
	}
}

// Un header de config no puede desarmar el transporte: si pudiera fijar
// Authorization, apagaría la credencial que el provider acaba de resolver.
func TestConfiguredHeadersCannotOverrideTheCredential(t *testing.T) {
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		Name: "gateway", Model: "model", BaseURL: "https://example.com", RequestURL: server.URL,
		APIKey:  "llave-real",
		Headers: map[string]string{"Authorization": "Bearer robada", "X-Ruta": "beta"},
	}).(*client)
	if _, err := client.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hola"}},
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	headers := <-seen
	if got := headers.Get("Authorization"); got != "Bearer llave-real" {
		t.Errorf("Authorization = %q, esperaba la credencial del provider", got)
	}
	if got := headers.Get("X-Ruta"); got != "beta" {
		t.Errorf("X-Ruta = %q, esperaba que el header propio sí pasara", got)
	}
}

// Regresión del prefijo de DeepSeek: su request no debe ganar ninguna clave
// nueva ni perder ninguna. Cualquier cambio aquí es un cache miss completo.
func TestDeepSeekRequestKeysAreUnchangedByTheCodexSupport(t *testing.T) {
	// "high" es el default del preset oficial, así que es la forma real del
	// request que la cache de DeepSeek ve turno a turno.
	client := New(Config{
		Name: "deepseek-responses", Model: "deepseek-v4-flash",
		BaseURL: "https://api.deepseek.com", Effort: "high",
	}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{
		Messages: conversationWithReasoning(),
		Tools:    []provider.ToolSchema{{Name: "bash", Description: "corre un comando"}},
	})

	got := slices.Sorted(maps.Keys(body))
	want := []string{"input", "max_output_tokens", "model", "reasoning", "stream", "tools"}
	if !slices.Equal(got, want) {
		t.Fatalf("claves del request de DeepSeek = %v, esperaba %v", got, want)
	}
}

// DeepSeek sigue reconstruyendo su item de razonamiento a partir del texto
// guardado: es lo que su documentación pide en turnos con herramientas, y es
// parte del prefijo que su cache reutiliza.
func TestDeepSeekStillRebuildsItsReasoningItem(t *testing.T) {
	client := New(Config{
		Name: "deepseek-responses", Model: "deepseek-v4-flash",
		BaseURL: "https://api.deepseek.com",
	}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: conversationWithReasoning()})

	var found bool
	for _, item := range body["input"].([]map[string]any) {
		if item["type"] != "reasoning" {
			continue
		}
		found = true
		if _, leaked := item["encrypted_content"]; leaked {
			t.Error("el item de DeepSeek llevó encrypted_content, que su wire no define")
		}
		content, _ := json.Marshal(item["content"])
		if !strings.Contains(string(content), "pensando") {
			t.Errorf("content = %s, esperaba el razonamiento guardado", content)
		}
	}
	if !found {
		t.Error("DeepSeek perdió su item de reasoning en el input")
	}
}

// El estado cifrado solo llega en el evento del item terminado, así que hay que
// capturarlo del stream: si no se guarda ahí, el turno siguiente no tiene nada
// que replicar y el modelo pierde su razonamiento entre llamadas a herramientas.
func TestChatGPTCapturesTheEncryptedReasoningItemFromTheStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.added","item":{"id":"rs_1","type":"reasoning"}}`,
			`{"type":"response.reasoning_text.delta","delta":"pensando"}`,
			`{"type":"response.output_item.done","item":{"id":"rs_1","type":"reasoning","encrypted_content":"OPAQUE","status":"completed"}}`,
			`{"type":"response.output_text.delta","delta":"listo"}`,
			`{"type":"response.completed","response":{"id":"resp_1"}}`,
		)
	}))
	defer server.Close()

	client := New(Config{
		Name: "chatgpt-codex", Model: "gpt-5.5",
		BaseURL: "https://chatgpt.com/backend-api/codex", RequestURL: server.URL,
	})
	chunks := collect(t, client, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hola"}},
	})

	var replayed json.RawMessage
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkResponsesItem {
			replayed = chunk.ResponsesItem
		}
	}
	if replayed == nil {
		t.Fatal("el stream no entregó el item de reasoning para replicarlo")
	}
	if !strings.Contains(string(replayed), "OPAQUE") {
		t.Errorf("item capturado = %s, esperaba el estado cifrado", replayed)
	}
}

// DeepSeek no pide razonamiento cifrado, así que sus items de reasoning no
// deben empezar a viajar por el canal de replay: eso duplicaría la cadena de
// pensamiento dentro de su prefijo cacheado.
func TestDeepSeekDoesNotReplayReasoningItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.done","item":{"id":"rs_1","type":"reasoning","status":"completed"}}`,
			`{"type":"response.output_text.delta","delta":"listo"}`,
			`{"type":"response.completed","response":{"id":"resp_1"}}`,
		)
	}))
	defer server.Close()

	client := New(Config{
		Name: "deepseek-responses", Model: "deepseek-v4-flash",
		BaseURL: "https://api.deepseek.com", RequestURL: server.URL,
	})
	for _, chunk := range collect(t, client, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hola"}},
	}) {
		if chunk.Type == provider.ChunkResponsesItem {
			t.Fatalf("DeepSeek replicó un item que antes no replicaba: %s", chunk.ResponsesItem)
		}
	}
}
