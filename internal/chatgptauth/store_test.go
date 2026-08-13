package chatgptauth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Cuando Reasonix no tiene sesión propia pero el Codex CLI sí, se reusa la
// suya: obliga a un `codex login` menos y es el caso de la primera vez.
func TestLoadFallsBackToTheCodexCLISession(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(fallback), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"OPENAI_API_KEY":null,"tokens":{"id_token":"a.b.c","access_token":"x.y.z","refresh_token":"rt","account_id":"acct"},"auth_mode":"chatgpt"}`
	if err := os.WriteFile(fallback, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := Store{Path: filepath.Join(dir, "reasonix", "auth.json"), Fallback: fallback}

	tokens, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tokens.RefreshToken != "rt" || tokens.AccountID != "acct" {
		t.Errorf("tokens = %+v, esperaba los del archivo ajeno", tokens)
	}
}

// Guardar nunca debe tocar el archivo del Codex CLI: son dos herramientas con
// su propia sesión y pisar la ajena rompería la de al lado.
func TestSaveWritesOnlyTheOwnFile(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(fallback), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := `{"tokens":{"id_token":"","access_token":"ajeno","refresh_token":"rt-ajeno"}}`
	if err := os.WriteFile(fallback, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := Store{Path: filepath.Join(dir, "reasonix", "auth.json"), Fallback: fallback}
	if err := store.Save(Tokens{AccessToken: "propio", RefreshToken: "rt-propio"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	if string(after) != original {
		t.Errorf("el archivo del Codex CLI cambió: %s", after)
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.AccessToken != "propio" {
		t.Errorf("AccessToken = %q, esperaba el propio", saved.AccessToken)
	}
}

// La sesión lleva credenciales: el archivo no puede quedar legible para otros
// usuarios de la máquina.
func TestSaveKeepsTheSessionPrivate(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "chatgpt", "auth.json")}
	if err := store.Save(Tokens{AccessToken: "a", RefreshToken: "b"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permisos = %o, esperaba 600", perm)
	}
}

// Sin ningún archivo, el error debe ser el que la CLI convierte en instrucción.
func TestLoadWithoutAnySessionReportsMissingCredentials(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "auth.json")}
	if _, err := store.Load(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("error = %v, esperaba ErrNoCredentials", err)
	}
}
