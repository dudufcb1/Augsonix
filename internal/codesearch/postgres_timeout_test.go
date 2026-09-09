package codesearch

import (
	"context"
	"testing"
	"time"
)

func TestPostgresPoolConfigAppliesDefaultConnectTimeout(t *testing.T) {
	// Un DSN que no dice nada sobre el plazo de conexión tiene que salir con el
	// default puesto. Sin él la apertura espera para siempre a un servidor caído,
	// que es lo que dejaba el arranque colgado en pantalla negra.
	t.Parallel()
	cfg, err := postgresPoolConfig("postgres://user:pass@db.example:5432/codesearch")
	if err != nil {
		t.Fatalf("postgresPoolConfig: %v", err)
	}
	if cfg.ConnConfig.ConnectTimeout != postgresConnectTimeout {
		t.Fatalf("ConnectTimeout = %v, se esperaba %v", cfg.ConnConfig.ConnectTimeout, postgresConnectTimeout)
	}
}

func TestPostgresPoolConfigKeepsExplicitConnectTimeout(t *testing.T) {
	// Si el DSN trae su propio connect_timeout es una decisión deliberada de
	// quien configuró la conexión, así que el default no debe pisarla.
	t.Parallel()
	cfg, err := postgresPoolConfig("postgres://user:pass@db.example:5432/codesearch?connect_timeout=3")
	if err != nil {
		t.Fatalf("postgresPoolConfig: %v", err)
	}
	if cfg.ConnConfig.ConnectTimeout != 3*time.Second {
		t.Fatalf("ConnectTimeout = %v, se esperaba 3s", cfg.ConnConfig.ConnectTimeout)
	}
}

func TestOpenPostgresStoreFailsInsteadOfHangingOnUnreachableHost(t *testing.T) {
	// Contra un host inalcanzable la apertura devuelve error en vez de quedarse
	// esperando: así boot puede desactivar code_search y seguir arrancando.
	// 192.0.2.1 es TEST-NET-1 (RFC 5737), reservada para pruebas y no enrutable.
	t.Parallel()
	start := time.Now()
	_, err := OpenPostgresStore(context.Background(),
		"postgres://user:pass@192.0.2.1:5432/codesearch?connect_timeout=1",
		"ws", "ws", "test-model", 8)
	if err == nil {
		t.Fatal("se esperaba error con el host inalcanzable")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("la apertura tardó %v; debía rendirse con el plazo de conexión", elapsed)
	}
}
