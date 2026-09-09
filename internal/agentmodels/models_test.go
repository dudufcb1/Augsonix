package agentmodels

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("escribir %s: %v", name, err)
	}
	return path
}

func TestReasonixListsProviderModelsWithTheirPrefix(t *testing.T) {
	// La bandera --model espera "proveedor/modelo": listar solo el nombre suelto
	// daría opciones que no se pueden usar.
	t.Parallel()
	path := write(t, "config.toml", `
default_model = "opencode/deepseek-v4-pro"

[[providers]]
name   = "opencode"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]

[[providers]]
name  = "codex"
model = "gpt-5.4"
`)
	models, err := Reasonix(path)
	if err != nil {
		t.Fatalf("Reasonix: %v", err)
	}
	values := map[string]bool{}
	for _, m := range models {
		values[m.Value] = true
	}
	for _, want := range []string{"opencode/deepseek-v4-flash", "opencode/deepseek-v4-pro", "codex/gpt-5.4"} {
		if !values[want] {
			t.Fatalf("falta %s en %+v", want, models)
		}
	}
}

func TestReasonixMarksTheDefaultModelFirst(t *testing.T) {
	// El modelo por defecto encabeza la lista para que sea el que se elige solo
	// cuando el usuario no toca el selector.
	t.Parallel()
	path := write(t, "config.toml", `
default_model = "opencode/deepseek-v4-pro"

[[providers]]
name   = "opencode"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
`)
	models, err := Reasonix(path)
	if err != nil {
		t.Fatalf("Reasonix: %v", err)
	}
	if len(models) == 0 || models[0].Value != "opencode/deepseek-v4-pro" || !models[0].Default {
		t.Fatalf("el default no encabeza: %+v", models)
	}
}

func TestClaudeAlwaysOffersTheStableAliases(t *testing.T) {
	// Los alias apuntan al modelo más reciente de cada familia, así que valen
	// aunque el JSON no tenga nada cacheado o ni siquiera exista.
	t.Parallel()
	models, _ := Claude(filepath.Join(t.TempDir(), "no-existe.json"))
	if len(models) < len(claudeAliases) {
		t.Fatalf("se perdieron los alias: %+v", models)
	}
	if models[0].Value != "fable" {
		t.Fatalf("el primer alias cambió: %+v", models[0])
	}
}

func TestClaudeAddsCachedPickerOptionsWithoutDuplicating(t *testing.T) {
	// El JSON trae los nombres completos con sufijo; se suman a los alias sin
	// repetir ninguno que ya estuviera.
	t.Parallel()
	path := write(t, "claude.json", `{"additionalModelOptionsCache":[
	  {"value":"claude-fable-5-1[1m]","label":"Fable","description":"Fable 5.1"},
	  {"value":"sonnet","label":"Sonnet","description":"duplicado del alias"}
	]}`)
	models, err := Claude(path)
	if err != nil {
		t.Fatalf("Claude: %v", err)
	}
	count := map[string]int{}
	for _, m := range models {
		count[m.Value]++
	}
	if count["claude-fable-5-1[1m]"] != 1 {
		t.Fatalf("no se agregó el modelo cacheado: %+v", models)
	}
	if count["sonnet"] != 1 {
		t.Fatalf("el alias quedó duplicado: %+v", models)
	}
}
