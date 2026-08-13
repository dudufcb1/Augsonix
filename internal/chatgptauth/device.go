package chatgptauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DeviceCode es lo que el usuario debe teclear en otro dispositivo.
type DeviceCode struct {
	UserCode string
	URL      string

	id       string
	interval time.Duration
}

// pollSafetyMargin se suma al intervalo que pide el servidor para no consumir
// el límite de sondeo cuando los relojes difieren.
const pollSafetyMargin = 3 * time.Second

// StartDeviceLogin pide un código de dispositivo. Es el camino sin navegador:
// sirve en un servidor por SSH, donde no hay a dónde redirigir el puerto 1455.
func StartDeviceLogin(ctx context.Context, client *http.Client) (DeviceCode, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := postJSON(ctx, client, Issuer+"/api/accounts/deviceauth/usercode", map[string]string{
		"client_id": ClientID,
	})
	if err != nil {
		return DeviceCode{}, fmt.Errorf("chatgptauth: iniciar el login por dispositivo: %w", err)
	}
	var parsed struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     string `json:"interval"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return DeviceCode{}, fmt.Errorf("chatgptauth: respuesta ilegible al pedir el código: %w", err)
	}
	seconds, _ := strconv.Atoi(strings.TrimSpace(parsed.Interval))
	if seconds < 1 {
		seconds = 5
	}
	return DeviceCode{
		UserCode: parsed.UserCode, URL: Issuer + "/codex/device",
		id: parsed.DeviceAuthID, interval: time.Duration(seconds) * time.Second,
	}, nil
}

// WaitDeviceLogin sondea hasta que el usuario autoriza el código y guarda la
// sesión. 403 y 404 significan "todavía no": cualquier otro estado es un fallo.
func WaitDeviceLogin(ctx context.Context, store Store, client *http.Client, device DeviceCode) (Tokens, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	for {
		status, body, err := postJSONStatus(ctx, client, Issuer+"/api/accounts/deviceauth/token", map[string]string{
			"device_auth_id": device.id,
			"user_code":      device.UserCode,
		})
		if err != nil {
			return Tokens{}, fmt.Errorf("chatgptauth: esperar la autorización del dispositivo: %w", err)
		}
		if status == http.StatusOK {
			var granted struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(body, &granted); err != nil {
				return Tokens{}, fmt.Errorf("chatgptauth: respuesta ilegible al autorizar: %w", err)
			}
			response, err := postForm(ctx, client, Issuer+"/oauth/token", url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {granted.AuthorizationCode},
				"redirect_uri":  {Issuer + "/deviceauth/callback"},
				"client_id":     {ClientID},
				"code_verifier": {granted.CodeVerifier},
			})
			if err != nil {
				return Tokens{}, fmt.Errorf("chatgptauth: canjear el código del dispositivo: %w", err)
			}
			tokens := mergeTokens(Tokens{}, response)
			if err := store.Save(tokens); err != nil {
				return tokens, err
			}
			return tokens, nil
		}
		if status != http.StatusForbidden && status != http.StatusNotFound {
			return Tokens{}, fmt.Errorf("chatgptauth: el login por dispositivo falló con estado %d: %s", status, snippet(body))
		}
		select {
		case <-ctx.Done():
			return Tokens{}, ctx.Err()
		case <-time.After(device.interval + pollSafetyMargin):
		}
	}
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload map[string]string) ([]byte, error) {
	status, body, err := postJSONStatus(ctx, client, endpoint, payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("el servidor respondió %d: %s", status, snippet(body))
	}
	return body, nil
}

func postJSONStatus(ctx context.Context, client *http.Client, endpoint string, payload map[string]string) (int, []byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}
