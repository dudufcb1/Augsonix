package config

import (
	"strings"
	"testing"
)

// El config se reescribe en forma canónica en cada guardado. Si el renderer no
// emite [codesearch], ese viaje apaga la búsqueda semántica sin avisar: fue
// exactamente lo que pasó el 2026-08-11, con el índice intacto en Postgres y la
// herramienta desaparecida de las sesiones.
func TestCodeSearchSurvivesARenderAndReloadRoundTrip(t *testing.T) {
	var cfg Config
	cfg.CodeSearch = DefaultCodeSearch()
	cfg.CodeSearch.Enabled = true
	cfg.CodeSearch.Backend = BackendPostgres
	cfg.CodeSearch.PostgresURLEnv = "CODESEARCH_POSTGRES_URL"
	cfg.CodeSearch.Dimensions = 2048
	cfg.CodeSearch.Containers = []string{"/home/edugoat/asistente"}

	rendered := RenderTOML(&cfg)
	if !strings.Contains(rendered, "[codesearch]") {
		t.Fatal("el config guardado no lleva la sección [codesearch]")
	}

	var reloaded Config
	if _, err := decodeTOMLBytes([]byte(rendered), &reloaded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := reloaded.CodeSearch
	if !got.Enabled {
		t.Error("enabled se perdió: la herramienta no se registraría")
	}
	if got.Backend != BackendPostgres {
		t.Errorf("backend = %q, esperaba postgres", got.Backend)
	}
	if got.PostgresURLEnv != "CODESEARCH_POSTGRES_URL" {
		t.Errorf("postgres_url_env = %q, se perdió la conexión al índice", got.PostgresURLEnv)
	}
	if got.Dimensions != 2048 {
		t.Errorf("dimensions = %d, esperaba 2048; otro valor invalida el índice", got.Dimensions)
	}
	if len(got.Containers) != 1 || got.Containers[0] != "/home/edugoat/asistente" {
		t.Errorf("containers = %v, se perdieron las carpetas contenedoras", got.Containers)
	}
}

// Un usuario que nunca tocó la sección no debe cargar con ella en su archivo.
func TestDefaultCodeSearchIsNotWrittenOut(t *testing.T) {
	var cfg Config
	cfg.CodeSearch = DefaultCodeSearch()
	if strings.Contains(RenderTOML(&cfg), "[codesearch]") {
		t.Error("se escribió la sección aunque está toda en su valor por defecto")
	}
}
