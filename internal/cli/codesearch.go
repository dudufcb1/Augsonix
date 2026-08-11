package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/codesearch"
	"reasonix/internal/config"
)

// codeSearchCommand administra el índice semántico. Existe porque el índice ya
// no vive en el proyecto: sin estos comandos no habría forma de ver qué hay
// guardado, ni de rehacerlo cuando queda a medias.
func codeSearchCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix codesearch <status|list|reindex [--force]|clear>")
		return 2
	}
	cfg, root, err := codeSearchContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	switch args[0] {
	case "status":
		return codeSearchStatus(cfg, root)
	case "reindex":
		return codeSearchReindex(cfg, root, hasFlag(args[1:], "--force"))
	case "clear":
		return codeSearchClear(cfg, root)
	case "list":
		return codeSearchList(cfg)
	default:
		fmt.Fprintf(os.Stderr, "codesearch: unknown operation %q\n", args[0])
		return 2
	}
}

// codeSearchContext carga la configuración y el workspace desde el cwd, que es
// donde el usuario espera que apunten estos comandos.
func codeSearchContext() (config.CodeSearchConfig, string, error) {
	root, err := os.Getwd()
	if err != nil {
		return config.CodeSearchConfig{}, "", err
	}
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		return config.CodeSearchConfig{}, "", err
	}
	return cfg.CodeSearch.Normalized(), root, nil
}

func codeSearchStatus(cfg config.CodeSearchConfig, root string) int {
	ws := codesearch.IdentifyWorkspace(root)
	fmt.Printf("proyecto   %s\n", ws.Name)
	fmt.Printf("identidad  %s  (de %s)\n", ws.ID, ws.Source)
	if !cfg.Enabled {
		fmt.Println("estado     apagado — activa [codesearch] enabled en reasonix.toml")
		return 0
	}
	fmt.Printf("modelo     %s a %d dimensiones\n", cfg.Model, cfg.Dimensions)
	fmt.Printf("backend    %s\n", cfg.Backend)
	// Cuántas credenciales hay a mano: con una sola, quedarse sin cuota detiene
	// el indexado; con varias entra la siguiente sin cortar el trabajo.
	switch n := boot.CodeSearchKeyring(cfg.APIKeyEnv).Len(); {
	case n == 0:
		fmt.Printf("credenciales ninguna — define %s\n", cfg.APIKeyEnv)
	case n == 1:
		fmt.Printf("credenciales 1 (sin relevo: agrega %s_2 para que el indexado siga si se agota)\n", cfg.APIKeyEnv)
	default:
		fmt.Printf("credenciales %d, con relevo automático\n", n)
	}

	store, err := openStoreForWorkspace(cfg, ws.ID, ws.Name)
	if err != nil {
		fmt.Printf("estado     inalcanzable: %v\n", err)
		return 1
	}
	defer closeStore(store)
	files, chunks := store.Stats()
	if chunks == 0 {
		fmt.Println("estado     vacío — se construye al abrir el proyecto")
		return 0
	}
	fmt.Printf("indexado   %d archivos, %d fragmentos\n", files, chunks)
	return 0
}

func codeSearchClear(cfg config.CodeSearchConfig, root string) int {
	ws := codesearch.IdentifyWorkspace(root)
	store, err := openStoreForWorkspace(cfg, ws.ID, ws.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	defer closeStore(store)

	paths := store.Paths()
	for _, p := range paths {
		store.Delete(p)
	}
	if err := store.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	// El estado local también se va: si sobreviviera, el próximo arranque
	// creería que todo sigue indexado y no reconstruiría nada.
	_ = os.RemoveAll(filepath.Join(config.CodeSearchIndexDir(), ws.ID))
	fmt.Printf("borrado el índice de %s (%d archivos). Se reconstruye al abrir el proyecto.\n", ws.Name, len(paths))
	return 0
}

// workspaceLister lo implementa el backend que sabe enumerar todo lo indexado,
// no solo el proyecto abierto. El almacén local no puede: cada proyecto es un
// directorio suelto sin nada que los relacione.
type workspaceLister interface {
	Workspaces() ([]codesearch.WorkspaceIndex, error)
}

// codeSearchList enumera los índices guardados. Sirve para encontrar los de
// proyectos que ya no se tocan, que de otro modo se quedan ocupando espacio sin
// que nadie sepa que están ahí.
func codeSearchList(cfg config.CodeSearchConfig) int {
	if cfg.Backend == config.BackendPostgres {
		return codeSearchListRemote(cfg)
	}
	dir := config.CodeSearchIndexDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("no hay índices locales guardados")
		return 0
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		fmt.Println("no hay índices locales guardados")
		return 0
	}
	fmt.Printf("índices en %s:\n", dir)
	for _, id := range ids {
		fmt.Printf("  %s  %s\n", id, dirSizeHuman(filepath.Join(dir, id)))
	}
	return 0
}

// codeSearchListRemote pregunta a la base, que es donde de verdad viven los
// vectores cuando el backend es remoto.
func codeSearchListRemote(cfg config.CodeSearchConfig) int {
	store, err := openStoreForWorkspace(cfg, "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	defer closeStore(store)
	lister, ok := store.(workspaceLister)
	if !ok {
		fmt.Println("este backend no sabe enumerar proyectos")
		return 0
	}
	all, err := lister.Workspaces()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	if len(all) == 0 {
		fmt.Println("no hay proyectos indexados")
		return 0
	}
	fmt.Printf("%-22s %-18s %8s %10s\n", "PROYECTO", "IDENTIDAD", "ARCHIVOS", "FRAGMENTOS")
	total := 0
	for _, w := range all {
		name := w.Name
		if name == "" {
			name = "(sin nombre)"
		}
		fmt.Printf("%-22s %-18s %8d %10d\n", name, w.Workspace, w.Files, w.Chunks)
		total += w.Chunks
	}
	fmt.Printf("%-22s %-18s %8s %10d\n", "total", "", "", total)
	return 0
}

func openStoreForWorkspace(cfg config.CodeSearchConfig, wsID, name string) (codesearch.VectorStore, error) {
	dir := filepath.Join(config.CodeSearchIndexDir(), wsID)
	return boot.OpenCodeSearchStore(context.Background(), dir, wsID, name, cfg)
}

// closeStore suelta las conexiones del backend remoto; el local no tiene qué
// cerrar y por eso la comprobación es por interfaz y no por tipo.
func closeStore(s codesearch.VectorStore) {
	if c, ok := s.(interface{ Close() }); ok {
		c.Close()
	}
}

func dirSizeHuman(dir string) string {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	switch {
	case total > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(total)/(1<<20))
	case total > 1<<10:
		return fmt.Sprintf("%.0f KB", float64(total)/(1<<10))
	default:
		return strings.TrimSpace(fmt.Sprintf("%d B", total))
	}
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}
