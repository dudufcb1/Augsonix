package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

const claudeStoreTestProviderKind = "boot-claude-store-test"

var (
	claudeStoreTestProviderOnce    sync.Once
	claudeStoreTestProviderMu      sync.Mutex
	claudeStoreTestProviderCurrent *testutil.MockProvider
)

func registerClaudeStoreTestProvider() {
	claudeStoreTestProviderOnce.Do(func() {
		provider.Register(claudeStoreTestProviderKind, func(provider.Config) (provider.Provider, error) {
			claudeStoreTestProviderMu.Lock()
			defer claudeStoreTestProviderMu.Unlock()
			if claudeStoreTestProviderCurrent == nil {
				return nil, errors.New("claude store test provider is not installed")
			}
			return claudeStoreTestProviderCurrent, nil
		})
	})
}

func setClaudeStoreTestProvider(t *testing.T, p *testutil.MockProvider) {
	t.Helper()
	claudeStoreTestProviderMu.Lock()
	claudeStoreTestProviderCurrent = p
	claudeStoreTestProviderMu.Unlock()
	t.Cleanup(func() {
		claudeStoreTestProviderMu.Lock()
		if claudeStoreTestProviderCurrent == p {
			claudeStoreTestProviderCurrent = nil
		}
		claudeStoreTestProviderMu.Unlock()
	})
}

// buildWithClaudeStore levanta un controlador real con la sección de config
// indicada y, si claudeHome no es vacío, apunta CLAUDE_CONFIG_DIR a ese home.
// seed, si no es nil, recibe el dir del proyecto (cwd del controlador) para
// sembrar fixtures antes del arranque. Devuelve el controlador y ese dir.
func buildWithClaudeStore(t *testing.T, section, claudeHome string, seed func(projectDir string)) (*control.Controller, string) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	if seed != nil {
		seed(dir)
	}
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+claudeStoreTestProviderKind+`"
model = "x"
`+section)

	registerClaudeStoreTestProvider()
	prov := testutil.NewMock(claudeStoreTestProviderKind, testutil.Turn{Text: "done"})
	setClaudeStoreTestProvider(t, prov)

	if claudeHome != "" {
		t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	}

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ctrl, dir
}

func seedClaudeStoreFor(t *testing.T, claudeHome, cwd string) {
	t.Helper()
	s := memory.StoreFor(claudeHome, cwd)
	if _, err := s.Save(memory.Memory{
		Name:        "cliente-margot",
		Title:       "Cliente Margot",
		Description: "Margot es la dueña de Converging Works",
		Type:        memory.TypeProject,
		Body:        "Margot es la dueña de Converging Works.",
	}); err != nil {
		t.Fatal(err)
	}
}

// claudeStoreSnapshot captura el contenido del store de un home de Claude para
// verificar que una sesión read-only no lo modifica.
func claudeStoreSnapshot(t *testing.T, claudeHome, cwd string) map[string]string {
	t.Helper()
	dir := memory.StoreFor(claudeHome, cwd).Dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	snapshot := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		snapshot[e.Name()] = string(b)
	}
	return snapshot
}

func registeredTools(ctrl *control.Controller) map[string]bool {
	out := map[string]bool{}
	for _, e := range ctrl.AllToolContractEntries() {
		out[e.Name] = true
	}
	return out
}

// El índice de Claude entra al prompt cacheado, la tool memory queda
// despachable y los writers no se registran: el árbol de Claude es solo
// lectura y la sesión no lo toca.
func TestClaudeStoreBridgesClaudeFactsIntoPromptReadOnly(t *testing.T) {
	claudeHome := t.TempDir()
	var before, after map[string]string
	ctrl, dir := buildWithClaudeStore(t, `
[memory]
claude_store = true
`, claudeHome, func(dir string) {
		seedClaudeStoreFor(t, claudeHome, dir)
		before = claudeStoreSnapshot(t, claudeHome, dir)
	})
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "hola"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	registered := registeredTools(ctrl)
	if !registered["memory"] {
		t.Error("la tool memory no está en el registry con claude_store activo")
	}
	for _, writer := range []string{"remember", "forget"} {
		if registered[writer] {
			t.Errorf("writer %q registrado con store externo: habilitaría escritura en el árbol de Claude", writer)
		}
	}

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "## Background memory index") || !strings.Contains(sys, "Cliente Margot") {
		t.Errorf("el índice de Claude no llegó al prompt del sistema:\n%s", sys)
	}

	after = claudeStoreSnapshot(t, claudeHome, dir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("el árbol de Claude cambió durante una sesión read-only:\nbefore=%v\nafter=%v", before, after)
	}
}

// Sin el flag, un home de Claude con memoria no se toca: nada entra al
// prompt y los writers se registran como siempre.
func TestClaudeStoreOffKeepsReasonixStoreAndWriters(t *testing.T) {
	claudeHome := t.TempDir()
	ctrl, _ := buildWithClaudeStore(t, "", claudeHome, func(dir string) {
		seedClaudeStoreFor(t, claudeHome, dir)
	})
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "hola"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	registered := registeredTools(ctrl)
	for _, writer := range []string{"remember", "forget"} {
		if !registered[writer] {
			t.Errorf("sin claude_store el writer %q debería estar registrado", writer)
		}
	}

	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "Cliente Margot") {
		t.Errorf("facts de Claude entraron al prompt sin claude_store:\n%s", sys)
	}
}
