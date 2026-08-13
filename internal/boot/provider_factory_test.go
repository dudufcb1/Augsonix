package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	// El kind lo registra el paquete del protocolo, igual que en el binario.
	_ "reasonix/internal/provider/responses"
)

// Un provider con api_key_env no recibe resolvedor de tokens: su camino de
// request debe quedar idéntico a como estaba antes de que el hook existiera.
func TestAPIKeyProvidersGetNoTokenSource(t *testing.T) {
	entry := config.ProviderEntry{
		Name: "deepseek-responses", Kind: "responses",
		BaseURL: "https://api.deepseek.com", APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	tokens, err := providerTokenSource(&entry, netclient.ProxySpec{Mode: netclient.ModeAuto})
	if err != nil {
		t.Fatalf("providerTokenSource: %v", err)
	}
	if tokens != nil {
		t.Error("una entrada con api_key_env recibió un resolvedor de OAuth")
	}
}

// El endpoint de la suscripción sí lo recibe, y el resolvedor explica qué
// hacer cuando todavía no hay sesión guardada.
func TestChatGPTEntryGetsATokenSourceThatDemandsLogin(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	entry := config.ProviderEntry{
		Name: "chatgpt-codex", Kind: "responses",
		BaseURL: "https://chatgpt.com/backend-api/codex",
	}
	tokens, err := providerTokenSource(&entry, netclient.ProxySpec{Mode: netclient.ModeAuto})
	if err != nil {
		t.Fatalf("providerTokenSource: %v", err)
	}
	if tokens == nil {
		t.Fatal("la entrada de suscripción no recibió resolvedor")
	}
	_, err = tokens(context.Background())
	if err == nil {
		t.Fatal("esperaba un error sin sesión guardada")
	}
	if !strings.Contains(err.Error(), "auth login") {
		t.Errorf("error = %v, esperaba que dijera cómo iniciar sesión", err)
	}
}

// El resolvedor llega al provider por Config.Extra, que es por donde viajan
// todas las opciones específicas de un kind.
func TestNewProviderPassesTheTokenSourceThroughExtra(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	entry := config.ProviderEntry{
		Name: "chatgpt-codex", Kind: "responses",
		BaseURL:    "https://chatgpt.com/backend-api/codex",
		RequestURL: "https://chatgpt.com/backend-api/codex/responses",
		Model:      "gpt-5.5",
	}
	if _, err := NewProvider(&entry); err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// La clave es parte del contrato entre boot y los providers.
	if provider.TokenSourceKey != "token_source" {
		t.Errorf("TokenSourceKey = %q, cambió el contrato con los providers", provider.TokenSourceKey)
	}
}
