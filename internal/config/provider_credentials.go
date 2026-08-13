package config

import (
	"net/netip"
	"net/url"
	"strings"
)

// Configured reports whether the provider is selectable. Providers that do not
// require an API key are configured by definition; providers that name an env var
// require that variable to resolve unless their endpoint is local/private.
func (e *ProviderEntry) Configured() bool {
	return e != nil && (!e.RequiresAPIKey() || e.APIKey() != "")
}

// RequiresAPIKey reports whether this provider should be hidden/validated when
// its configured api_key_env is empty. A blank api_key_env means the provider is
// intentionally no-auth. Local OpenAI-compatible gateways often keep a legacy
// api_key_env in config even though they accept unauthenticated requests, so
// loopback/private endpoints are also allowed to run without a resolved key.
func (e *ProviderEntry) RequiresAPIKey() bool {
	if e == nil {
		return false
	}
	if strings.TrimSpace(e.APIKeyEnv) == "" {
		return providerBaseURLRequiresAPIKey(e.BaseURL)
	}
	return !providerBaseURLAllowsMissingAPIKey(e.BaseURL)
}

func providerBaseURLRequiresAPIKey(raw string) bool {
	switch officialProviderHost(raw) {
	case "api.deepseek.com", "api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com", "api.minimaxi.com", "api.openai.com":
		return true
	default:
		return false
	}
}

func providerBaseURLAllowsMissingAPIKey(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// chatGPTSessionHosts atiende únicamente a la suscripción: no existe una API
// key que sirva contra ellos, así que la credencial solo puede ser la sesión.
func chatGPTSessionHosts(host string) bool {
	return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com")
}

// UsesChatGPTSession reporta si la credencial de esta entrada es la sesión de
// una suscripción ChatGPT en vez de una llave. Se deriva del endpoint, como el
// resto del comportamiento por proveedor, para que no exista una configuración
// donde el mecanismo declarado y el destino real se contradigan. Una llave
// explícita gana: un gateway propio delante del host sigue siendo posible.
func (e *ProviderEntry) UsesChatGPTSession() bool {
	if e == nil || strings.TrimSpace(e.APIKeyEnv) != "" {
		return false
	}
	for _, raw := range []string{e.BaseURL, e.RequestURL} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if chatGPTSessionHosts(strings.ToLower(parsed.Hostname())) {
			return true
		}
	}
	return false
}
