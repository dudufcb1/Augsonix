package config

import "reasonix/internal/provider"

// chatGPTCodexModels son los modelos verificados contra el backend de Codex con
// un plan Plus. El resto de la familia gpt-5 —incluidos los -codex y -pro—
// responde "not supported when using Codex with a ChatGPT account": qué se
// habilita depende del plan, así que la lista es editable y `reasonix auth
// status` dice con qué plan está la sesión.
var chatGPTCodexModels = []string{"gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.5", "gpt-5.4", "gpt-5.4-mini"}

// chatGPTCodexPreset habla con la suscripción de ChatGPT en vez de la API de
// pago: no lleva api_key_env porque su credencial es la sesión OAuth que
// guarda `reasonix auth login openai`.
var chatGPTCodexPreset = ProviderPreset{
	ID:          "chatgpt-codex",
	Label:       "ChatGPT Plus/Pro (Codex)",
	Description: "Backend de Codex con la suscripción de ChatGPT. Sin api_key: corre `reasonix auth login openai`.",
	Entries: []ProviderEntry{{
		Name:    "chatgpt-codex",
		Kind:    "responses",
		BaseURL: "https://chatgpt.com/backend-api/codex",
		// El endpoint no cuelga de /v1 como el resto de la Responses API, y su
		// host es el que hace que la credencial sea la sesión de la suscripción.
		RequestURL: "https://chatgpt.com/backend-api/codex/responses",
		Models:     chatGPTCodexModels,
		Default:    "gpt-5.6-sol",
		// La ventana anunciada de 400K incluye los 128K de salida. Reasonix mide
		// context_window contra la entrada, así que va el techo de entrada real.
		ContextWindow: 272_000,
		ResponsesMode: "stateless",
		// El servidor enumera los suyos al rechazar uno inválido: minimal no
		// entra, y max sí. El default iguala al del Codex CLI.
		SupportedEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"},
		DefaultEffort:    "medium",
		BillingMode:      "subscription_equivalent",
		BillingCurrency:  "USD",
		Price:            &provider.Pricing{},
	}},
}

// subscriptionProviderPresets son los presets cuya credencial no es una llave
// sino una sesión de suscripción. Se componen aparte del catálogo literal para
// que agregar uno no toque la tabla de proveedores por API key.
func subscriptionProviderPresets() []ProviderPreset {
	return []ProviderPreset{chatGPTCodexPreset}
}
