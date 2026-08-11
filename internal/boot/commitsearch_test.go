package boot

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/codesearch"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/tool"
)

func TestCommitSearchStaysOutOfTheRegistryWhenDisabled(t *testing.T) {
	// Una tool registrada viaja en el prefijo del prompt en cada turno de cada
	// sesión, aunque nadie la llame. Si el índice de historia está apagado, su
	// esquema no debe llegar al registro.
	reg := tool.NewRegistry()
	cfg := config.CodeSearchConfig{Enabled: true, Commits: false}
	addCommitSearch(context.Background(), reg, t.TempDir(), cfg, codesearch.NewKeyring("clave"), netclient.ProxySpec{}, io.Discard)
	if _, ok := reg.Get("git_commit_search"); ok {
		t.Error("la tool se registró con la historia desactivada")
	}
}

func TestExampleConfigParsesWithCommitOptions(t *testing.T) {
	// El archivo de ejemplo es lo que la gente copia. Si una opción quedara mal
	// escrita ahí, se descubriría en la máquina de quien lo use.
	path := filepath.Join("..", "..", "reasonix.example.toml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("sin archivo de ejemplo")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		CodeSearch config.CodeSearchConfig `toml:"codesearch"`
	}
	meta, err := toml.Decode(string(data), &parsed)
	if err != nil {
		t.Fatalf("el ejemplo no parsea: %v", err)
	}
	for _, key := range [][]string{{"codesearch", "commits"}, {"codesearch", "commit_limit"}} {
		if !meta.IsDefined(key...) {
			t.Errorf("el ejemplo no documenta %v", key)
		}
	}
	if parsed.CodeSearch.Commits {
		t.Error("el ejemplo trae la historia encendida por defecto; cuesta cuota y debe optarse")
	}
}

func TestCodeSearchGuidanceStaysOutOfContainerFolders(t *testing.T) {
	// En una carpeta contenedora la herramienta no se registra, así que su guía
	// tampoco puede entrar al prompt: serían instrucciones sobre algo que el
	// modelo no va a tener, pagadas en el prefijo de cada turno.
	root := t.TempDir()
	t.Setenv("CODESEARCH_TEST_KEY", "clave")
	cfg := config.CodeSearchConfig{Enabled: true, APIKeyEnv: "CODESEARCH_TEST_KEY"}
	if !codeSearchAvailable(cfg, root) {
		t.Fatal("no se dio por disponible en una carpeta normal")
	}
	cfg.Containers = []string{root}
	if codeSearchAvailable(cfg, root) {
		t.Error("se dio por disponible en la raíz de un contenedor")
	}
	if !codeSearchAvailable(cfg, filepath.Join(root, "subproyecto")) {
		t.Error("se negó dentro de un subproyecto del contenedor")
	}
}
