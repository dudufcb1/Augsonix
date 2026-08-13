package chatgptauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tokens es el juego de credenciales de una sesión de ChatGPT. AccountID viaja
// en el header ChatGPT-Account-Id y sale de los claims del token.
type Tokens struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
	AccountID    string
	LastRefresh  time.Time
}

// Valid reporta si hay material suficiente para intentar una request o un
// refresco. Sin refresh token la sesión muere en cuanto vence el acceso.
func (t Tokens) Valid() bool {
	return strings.TrimSpace(t.AccessToken) != "" || strings.TrimSpace(t.RefreshToken) != ""
}

// ErrNoCredentials indica que no hay sesión guardada en ningún origen conocido.
var ErrNoCredentials = errors.New("no hay sesión de ChatGPT: corre `reasonix auth login openai`")

// authFile es el formato en disco. Es deliberadamente el mismo archivo que
// escribe el Codex CLI, para poder leer su sesión sin traducir nada.
type authFile struct {
	OpenAIAPIKey *string    `json:"OPENAI_API_KEY"`
	Tokens       fileTokens `json:"tokens"`
	LastRefresh  string     `json:"last_refresh,omitempty"`
	AuthMode     string     `json:"auth_mode,omitempty"`
}

type fileTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
}

// Store lee y escribe la sesión. Path es el archivo propio de Reasonix;
// Fallback es un archivo ajeno que solo se lee (el del Codex CLI).
type Store struct {
	Path     string
	Fallback string
}

// Load devuelve la sesión guardada. Prefiere el archivo propio y cae al ajeno
// cuando el propio no existe todavía, para que un `codex login` previo sirva.
func (s Store) Load() (Tokens, error) {
	if tokens, err := readAuthFile(s.Path); err == nil {
		return tokens, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Tokens{}, err
	}
	if strings.TrimSpace(s.Fallback) == "" {
		return Tokens{}, ErrNoCredentials
	}
	tokens, err := readAuthFile(s.Fallback)
	if errors.Is(err, os.ErrNotExist) {
		return Tokens{}, ErrNoCredentials
	}
	return tokens, err
}

// Save escribe la sesión en el archivo propio, nunca en el del Codex CLI: una
// sesión de Reasonix no debe pisar la credencial de otra herramienta.
func (s Store) Save(tokens Tokens) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("chatgptauth: no se pudo resolver dónde guardar la sesión")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("chatgptauth: preparar el directorio de estado: %w", err)
	}
	payload := authFile{
		Tokens: fileTokens{
			IDToken: tokens.IDToken, AccessToken: tokens.AccessToken,
			RefreshToken: tokens.RefreshToken, AccountID: tokens.AccountID,
		},
		LastRefresh: tokens.LastRefresh.UTC().Format(time.RFC3339),
		AuthMode:    "chatgpt",
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("chatgptauth: serializar la sesión: %w", err)
	}
	temp := s.Path + ".tmp"
	if err := os.WriteFile(temp, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("chatgptauth: escribir la sesión: %w", err)
	}
	if err := os.Rename(temp, s.Path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("chatgptauth: publicar la sesión: %w", err)
	}
	return nil
}

// Clear borra la sesión propia. El archivo ajeno queda intacto.
func (s Store) Clear() error {
	if strings.TrimSpace(s.Path) == "" {
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("chatgptauth: borrar la sesión: %w", err)
	}
	return nil
}

func readAuthFile(path string) (Tokens, error) {
	if strings.TrimSpace(path) == "" {
		return Tokens{}, os.ErrNotExist
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Tokens{}, err
	}
	var parsed authFile
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Tokens{}, fmt.Errorf("chatgptauth: %s no tiene el formato esperado: %w", path, err)
	}
	tokens := Tokens{
		IDToken: parsed.Tokens.IDToken, AccessToken: parsed.Tokens.AccessToken,
		RefreshToken: parsed.Tokens.RefreshToken, AccountID: parsed.Tokens.AccountID,
	}
	if tokens.AccountID == "" {
		tokens.AccountID = AccountID(tokens)
	}
	if parsed.LastRefresh != "" {
		if when, err := time.Parse(time.RFC3339, parsed.LastRefresh); err == nil {
			tokens.LastRefresh = when
		}
	}
	if !tokens.Valid() {
		return Tokens{}, ErrNoCredentials
	}
	return tokens, nil
}
