package chatgptauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// claims son los campos del JWT que nos interesan. El resto se ignora.
type claims struct {
	Expiry      int64     `json:"exp"`
	OpenAIAuth  authClaim `json:"https://api.openai.com/auth"`
	ChatGPTAcct string    `json:"chatgpt_account_id"`
	Orgs        []struct {
		ID string `json:"id"`
	} `json:"organizations"`
}

type authClaim struct {
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	ChatGPTPlanType  string `json:"chatgpt_plan_type"`
}

// AccountID extrae la cuenta a la que se le cobra el uso. OpenAI la publica en
// tres lugares distintos según cómo se haya emitido el token, así que se buscan
// los tres en el mismo orden que el Codex CLI.
func AccountID(tokens Tokens) string {
	for _, token := range []string{tokens.IDToken, tokens.AccessToken} {
		parsed, ok := parseClaims(token)
		if !ok {
			continue
		}
		switch {
		case parsed.ChatGPTAcct != "":
			return parsed.ChatGPTAcct
		case parsed.OpenAIAuth.ChatGPTAccountID != "":
			return parsed.OpenAIAuth.ChatGPTAccountID
		case len(parsed.Orgs) > 0 && parsed.Orgs[0].ID != "":
			return parsed.Orgs[0].ID
		}
	}
	return ""
}

// PlanType es el plan de la cuenta ("plus", "pro", "free"). El backend de Codex
// rechaza todo modelo cuando el plan es free, así que sirve para explicar el
// error antes de que el usuario lo vea como un 400 sin contexto.
func PlanType(tokens Tokens) string {
	for _, token := range []string{tokens.AccessToken, tokens.IDToken} {
		if parsed, ok := parseClaims(token); ok && parsed.OpenAIAuth.ChatGPTPlanType != "" {
			return parsed.OpenAIAuth.ChatGPTPlanType
		}
	}
	return ""
}

// ExpiresAt es el vencimiento del token de acceso. Cero cuando el JWT no se
// puede leer, lo que fuerza un refresco en vez de confiar en un token opaco.
func ExpiresAt(accessToken string) time.Time {
	parsed, ok := parseClaims(accessToken)
	if !ok || parsed.Expiry <= 0 {
		return time.Time{}
	}
	return time.Unix(parsed.Expiry, 0)
}

func parseClaims(token string) (claims, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return claims{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, false
	}
	var parsed claims
	if err := json.Unmarshal(body, &parsed); err != nil {
		return claims{}, false
	}
	return parsed, true
}
