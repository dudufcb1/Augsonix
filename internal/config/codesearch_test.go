package config

import (
	"strings"
	"testing"
)

func TestDefaultCodeSearchIsDisabled(t *testing.T) {
	// Indexar gasta la cuota del usuario en su proveedor de embeddings, así que
	// la función arranca apagada y se activa a propósito.
	if DefaultCodeSearch().Enabled {
		t.Error("la búsqueda semántica viene activada por defecto")
	}
}

func TestNormalizedFillsMissingValues(t *testing.T) {
	// Un archivo de configuración a medio escribir no debe dejar el índice
	// inservible: lo que falte se completa con los valores por defecto.
	got := CodeSearchConfig{Enabled: true}.Normalized()
	d := DefaultCodeSearch()
	if got.Model != d.Model {
		t.Errorf("Model = %q, esperaba %q", got.Model, d.Model)
	}
	if got.Dimensions != d.Dimensions {
		t.Errorf("Dimensions = %d, esperaba %d", got.Dimensions, d.Dimensions)
	}
	if got.APIKeyEnv != d.APIKeyEnv {
		t.Errorf("APIKeyEnv = %q, esperaba %q", got.APIKeyEnv, d.APIKeyEnv)
	}
}

func TestNormalizedRejectsUnknownBackendAndMode(t *testing.T) {
	// Un valor mal escrito cae al valor por defecto en vez de propagarse: un
	// backend inexistente reventaría al arrancar, y un modo desconocido dejaría
	// el prompt sin la guía que el usuario creía haber activado.
	got := CodeSearchConfig{Backend: "mongo", Prompt: "siempre"}.Normalized()
	if got.Backend != BackendLocal {
		t.Errorf("Backend = %q, esperaba caer a %q", got.Backend, BackendLocal)
	}
	if got.Prompt != PromptModeTool {
		t.Errorf("Prompt = %q, esperaba caer a %q", got.Prompt, PromptModeTool)
	}
}

func TestNormalizedKeepsValidValues(t *testing.T) {
	// Lo que sí es válido se respeta tal cual.
	got := CodeSearchConfig{Backend: BackendPostgres, Prompt: PromptModeMandatory, Dimensions: 2048}.Normalized()
	if got.Backend != BackendPostgres || got.Prompt != PromptModeMandatory || got.Dimensions != 2048 {
		t.Errorf("se alteró una configuración válida: %+v", got)
	}
}

func TestPromptGuidanceVariesByMode(t *testing.T) {
	// El modo barato no toca el prompt; los otros dos agregan texto, y el
	// obligatorio tiene que ser el más explícito porque es el que consigue que
	// el modelo consulte el índice antes de editar.
	if g := (CodeSearchConfig{Prompt: PromptModeTool}).PromptGuidance(); g != "" {
		t.Errorf("el modo tool no debe tocar el prompt, agregó %q", g)
	}
	enc := (CodeSearchConfig{Prompt: PromptModeEncourage}).PromptGuidance()
	man := (CodeSearchConfig{Prompt: PromptModeMandatory}).PromptGuidance()
	if enc == "" || man == "" {
		t.Fatal("los modos encourage y mandatory deben aportar guía")
	}
	if len(man) <= len(enc) {
		t.Errorf("el modo obligatorio (%d chars) debería ser más explícito que el sugerido (%d)", len(man), len(enc))
	}
}

func TestPromptGuidanceIsStable(t *testing.T) {
	// El texto entra al prefijo estable del prompt. Si variara entre llamadas
	// dentro de una sesión, cada turno sería un fallo de caché del proveedor.
	c := CodeSearchConfig{Prompt: PromptModeMandatory}
	// Varias vueltas, no dos: un texto construido recorriendo un mapa sale
	// igual dos veces a menudo y distinto a la décima.
	first := c.PromptGuidance()
	for i := range 10 {
		if got := c.PromptGuidance(); got != first {
			t.Fatalf("la guía cambió en la llamada %d:\n primera: %q\n ahora:   %q", i+2, first, got)
		}
	}
}

func TestConfigDefaultIncludesCodeSearch(t *testing.T) {
	// La sección tiene que existir en la configuración por defecto para que el
	// archivo generado la muestre y sea descubrible.
	if Default().CodeSearch.Model == "" {
		t.Error("la configuración por defecto no trae la sección codesearch")
	}
}

func TestWarningsCatchTypedValues(t *testing.T) {
	// El caso real: alguien escribe grep_friction = "true" creyendo que activa
	// la fricción. Normalized lo cae a "off" para que el índice siga usable,
	// pero callarse ahí deja al usuario creyendo que configuró algo que no.
	got := CodeSearchConfig{GrepFriction: "true", Prompt: "siempre", Backend: "mongo"}.Warnings()
	if len(got) != 3 {
		t.Fatalf("se avisó de %d valores inválidos, esperaba 3: %v", len(got), got)
	}
	joined := strings.Join(got, " | ")
	for _, want := range []string{"grep_friction", "prompt_mode", "backend", "off", "tool", "local"} {
		if !strings.Contains(joined, want) {
			t.Errorf("el aviso no menciona %q: %s", want, joined)
		}
	}
}

func TestWarningsStayQuietForValidConfig(t *testing.T) {
	// Una configuración correcta, o con campos vacíos que toman su default, no
	// debe generar ruido en cada arranque.
	for _, c := range []CodeSearchConfig{
		{},
		{Backend: BackendPostgres, Prompt: PromptModeMandatory, GrepFriction: FrictionStrict},
	} {
		if got := c.Warnings(); len(got) != 0 {
			t.Errorf("configuración válida avisó %v", got)
		}
	}
}
