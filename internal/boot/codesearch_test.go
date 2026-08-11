package boot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const codeSearchTestProviderKind = "boot-codesearch-test"

var (
	codeSearchTestProviderOnce    sync.Once
	codeSearchTestProviderMu      sync.Mutex
	codeSearchTestProviderCurrent *testutil.MockProvider
)

func registerCodeSearchTestProvider() {
	codeSearchTestProviderOnce.Do(func() {
		provider.Register(codeSearchTestProviderKind, func(provider.Config) (provider.Provider, error) {
			codeSearchTestProviderMu.Lock()
			defer codeSearchTestProviderMu.Unlock()
			if codeSearchTestProviderCurrent == nil {
				return nil, errors.New("codesearch test provider is not installed")
			}
			return codeSearchTestProviderCurrent, nil
		})
	})
}

func setCodeSearchTestProvider(t *testing.T, p *testutil.MockProvider) {
	t.Helper()
	codeSearchTestProviderMu.Lock()
	codeSearchTestProviderCurrent = p
	codeSearchTestProviderMu.Unlock()
	t.Cleanup(func() {
		codeSearchTestProviderMu.Lock()
		if codeSearchTestProviderCurrent == p {
			codeSearchTestProviderCurrent = nil
		}
		codeSearchTestProviderMu.Unlock()
	})
}

// buildWithCodeSearch levanta un controlador real con la sección [codesearch]
// indicada y devuelve las tools que llegaron al primer request del proveedor
// junto con el prompt del sistema que se ensambló.
func buildWithCodeSearch(t *testing.T, section string) ([]string, string) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+codeSearchTestProviderKind+`"
model = "x"
`+section)

	registerCodeSearchTestProvider()
	prov := testutil.NewMock(codeSearchTestProviderKind, testutil.Turn{Text: "done"})
	setCodeSearchTestProvider(t, prov)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "hola"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) == 0 {
		t.Fatal("el proveedor no recibió requests")
	}
	return toolSchemaNames(reqs[0].Tools), systemMessage(ctrl.History())
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestCodeSearchAbsentByDefault(t *testing.T) {
	// Sin configurar, la tool no debe llegar al proveedor: su schema pesa ~223
	// tokens en el prefijo de cada turno de cada sesión, y quien no la
	// configuró no tiene por qué pagarlos.
	names, _ := buildWithCodeSearch(t, "")
	if hasName(names, "code_search") {
		t.Errorf("code_search llegó al proveedor sin estar configurada; tools=%v", names)
	}
}

func TestCodeSearchAbsentWithoutAPIKey(t *testing.T) {
	// Habilitada pero sin credencial, la tool no puede responder. Registrarla
	// costaría prefijo y solo produciría errores en cada llamada.
	t.Setenv("CODESEARCH_TEST_KEY", "")
	names, _ := buildWithCodeSearch(t, `
[codesearch]
enabled = true
api_key_env = "CODESEARCH_TEST_KEY"
`)
	if hasName(names, "code_search") {
		t.Errorf("code_search se registró sin credencial; tools=%v", names)
	}
}

func TestCodeSearchReachesProviderWhenConfigured(t *testing.T) {
	// Con configuración y credencial, la tool tiene que llegar al request real
	// del proveedor: registrarla en el registro interno no sirve de nada si no
	// aparece en el contrato que ve el modelo.
	t.Setenv("CODESEARCH_TEST_KEY", "clave-de-prueba")
	names, _ := buildWithCodeSearch(t, `
[codesearch]
enabled = true
api_key_env = "CODESEARCH_TEST_KEY"
`)
	if !hasName(names, "code_search") {
		t.Errorf("code_search no llegó al proveedor estando configurada; tools=%v", names)
	}
}

func TestCodeSearchIndexLivesUnderWorkspaceIdentity(t *testing.T) {
	// El índice se guarda bajo la identidad del workspace, no bajo su ruta, para
	// que mover la carpeta no obligue a reindexar y volver a pagar los
	// embeddings.
	dir := t.TempDir()
	base := filepath.Base(indexDirForTest(dir))
	if base == "" || base == "codesearch" {
		t.Errorf("el índice no quedó bajo un identificador de workspace: %q", base)
	}
}

func TestCodeSearchMandatoryModeReachesSystemPrompt(t *testing.T) {
	// El modo obligatorio existe para que el modelo consulte el índice antes de
	// editar. Si su texto no llega al prompt del sistema, la perilla no hace
	// nada y el modo es una ilusión.
	t.Setenv("CODESEARCH_TEST_KEY", "clave-de-prueba")
	_, sys := buildWithCodeSearch(t, `
[codesearch]
enabled = true
api_key_env = "CODESEARCH_TEST_KEY"
prompt_mode = "mandatory"
`)
	if !strings.Contains(sys, "call code_search first") {
		t.Errorf("la guía del modo obligatorio no llegó al prompt:\n%s", sys)
	}
}

func TestCodeSearchToolModeLeavesPromptAlone(t *testing.T) {
	// El modo barato no debe tocar el prompt: su razón de existir es no pagar
	// tokens fijos en cada turno de cada sesión.
	t.Setenv("CODESEARCH_TEST_KEY", "clave-de-prueba")
	_, sys := buildWithCodeSearch(t, `
[codesearch]
enabled = true
api_key_env = "CODESEARCH_TEST_KEY"
prompt_mode = "tool"
`)
	if strings.Contains(sys, "code_search") {
		t.Errorf("el modo tool agregó guía al prompt:\n%s", sys)
	}
}

func TestCodeSearchGuidanceOmittedWhenToolAbsent(t *testing.T) {
	// Sin credencial la tool no se registra. Dejar la instrucción mandaría al
	// modelo a llamar algo que no existe, y gastaría un turno en descubrirlo.
	t.Setenv("CODESEARCH_TEST_KEY", "")
	_, sys := buildWithCodeSearch(t, `
[codesearch]
enabled = true
api_key_env = "CODESEARCH_TEST_KEY"
prompt_mode = "mandatory"
`)
	if strings.Contains(sys, "code_search") {
		t.Errorf("se instruyó sobre una tool que no se registró:\n%s", sys)
	}
}
