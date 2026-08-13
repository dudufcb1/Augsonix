package chatgptauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAccessToken arma un JWT sin firma válida (nadie la verifica localmente)
// con el vencimiento y los claims de cuenta que el código sí lee.
func fakeAccessToken(t *testing.T, expiry time.Time, account, plan string) string {
	t.Helper()
	payload := map[string]any{
		"exp": expiry.Unix(),
		"https://api.openai.com/auth": map[string]string{
			"chatgpt_account_id": account,
			"chatgpt_plan_type":  plan,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return "x." + base64.RawURLEncoding.EncodeToString(body) + ".y"
}

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "chatgpt", "auth.json")}
}

// Un token con vida de sobra se entrega tal cual: refrescar de más gasta una
// llamada al issuer en cada turno y puede toparse con su límite de tasa.
func TestCredentialReusesTokenThatIsStillFresh(t *testing.T) {
	store := newStore(t)
	access := fakeAccessToken(t, time.Now().Add(time.Hour), "acct-1", "pro")
	if err := store.Save(Tokens{AccessToken: access, RefreshToken: "rt", AccountID: "acct-1"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	source := NewSource(store, nil)
	source.issuer = "http://127.0.0.1:0" // cualquier llamada de red haría fallar la prueba

	got, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if got.AccessToken != access {
		t.Errorf("AccessToken = %q, esperaba el guardado", got.AccessToken)
	}
	if got.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, esperaba acct-1", got.AccountID)
	}
}

// Un token vencido se cambia por uno nuevo y la sesión renovada queda en disco:
// si no se guardara, cada arranque volvería a pedirle un refresh al issuer.
func TestCredentialRefreshesExpiredTokenAndPersistsIt(t *testing.T) {
	fresh := fakeAccessToken(t, time.Now().Add(time.Hour), "acct-2", "plus")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, esperaba refresh_token", got)
		}
		if got := r.Form.Get("client_id"); got != ClientID {
			t.Errorf("client_id = %q, esperaba el del Codex CLI", got)
		}
		fmt.Fprintf(w, `{"access_token":%q,"id_token":%q,"expires_in":3600}`, fresh, fresh)
	}))
	defer server.Close()

	store := newStore(t)
	stale := fakeAccessToken(t, time.Now().Add(-time.Minute), "acct-2", "plus")
	if err := store.Save(Tokens{AccessToken: stale, RefreshToken: "rt-viejo"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	source := NewSource(store, server.Client())
	source.issuer = server.URL

	got, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if got.AccessToken != fresh {
		t.Errorf("AccessToken = %q, esperaba el refrescado", got.AccessToken)
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.AccessToken != fresh {
		t.Errorf("token guardado = %q, esperaba el refrescado", saved.AccessToken)
	}
	// El issuer no reemitió refresh token: perderlo obligaría a un login nuevo.
	if saved.RefreshToken != "rt-viejo" {
		t.Errorf("refresh token = %q, esperaba conservar el anterior", saved.RefreshToken)
	}
}

// Diez requests en paralelo con el token vencido deben producir un solo
// refresco: el issuer limita la tasa y un turno con herramientas dispara varias
// llamadas casi simultáneas.
func TestConcurrentCredentialsRefreshOnlyOnce(t *testing.T) {
	fresh := fakeAccessToken(t, time.Now().Add(time.Hour), "acct-3", "pro")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		fmt.Fprintf(w, `{"access_token":%q,"expires_in":3600}`, fresh)
	}))
	defer server.Close()

	store := newStore(t)
	stale := fakeAccessToken(t, time.Now().Add(-time.Minute), "acct-3", "pro")
	if err := store.Save(Tokens{AccessToken: stale, RefreshToken: "rt"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	source := NewSource(store, server.Client())
	source.issuer = server.URL

	done := make(chan error, 10)
	for range 10 {
		go func() {
			_, err := source.Credential(context.Background())
			done <- err
		}()
	}
	for range 10 {
		if err := <-done; err != nil {
			t.Fatalf("Credential: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("refrescos = %d, esperaba 1", got)
	}
}

// Sin sesión guardada el error debe decir qué hacer: el usuario que estrena el
// provider ve este mensaje antes que cualquier respuesta del servidor.
func TestCredentialWithoutSessionExplainsHowToLogIn(t *testing.T) {
	source := NewSource(newStore(t), nil)
	if _, err := source.Credential(context.Background()); err == nil {
		t.Fatal("esperaba error sin sesión guardada")
	} else if err.Error() != ErrNoCredentials.Error() {
		t.Errorf("error = %v, esperaba el de sesión ausente", err)
	}
}
