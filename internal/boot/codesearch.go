package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/codesearch"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/netclient"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// codeSearchHTTPTimeout cubre un lote de embeddings, que con textos largos y el
// límite de tasa del proveedor tarda bastante más que una llamada normal.
const codeSearchHTTPTimeout = 2 * time.Minute

// addCodeSearch registra la búsqueda semántica cuando está configurada, y deja
// el índice sincronizándose en segundo plano. Devuelve nil sin tocar el
// registro si está apagada o le falta la credencial: una tool que no puede
// responder seguiría costando tokens de prefijo en cada turno.
func addCodeSearch(ctx context.Context, reg *tool.Registry, root string, cfg config.CodeSearchConfig, proxy netclient.ProxySpec, stderr io.Writer, gate *builtin.SearchGate, watcher *codesearch.Watcher) *codesearch.Index {
	if !cfg.Enabled || root == "" {
		return nil
	}
	for _, w := range cfg.Warnings() {
		fmt.Fprintln(stderr, w)
	}
	// Una carpeta contenedora agrupa proyectos; indexarla entera arrastra todo
	// lo que cuelga de ella. Sus subcarpetas sí se indexan por separado.
	if cfg.IsContainer(root) {
		fmt.Fprintf(stderr, "code_search disabled: %s está listado como contenedor en [codesearch] containers; abre un subproyecto para indexarlo\n", root)
		return nil
	}
	cfg = cfg.Normalized()
	keys := CodeSearchKeyring(cfg.APIKeyEnv)
	if keys.Len() == 0 {
		fmt.Fprintf(stderr, "code_search disabled: %s is not set\n", cfg.APIKeyEnv)
		return nil
	}
	keys.OnRetire(func(spent, alive, total int) {
		fmt.Fprintf(stderr, "codesearch: se agotó la cuota de la credencial %d de %d; quedan %d\n", spent, total, alive)
	})

	ix, err := OpenCodeSearchIndex(ctx, root, cfg, keys, proxy)
	if err != nil {
		fmt.Fprintf(stderr, "code_search disabled: %v\n", err)
		return nil
	}
	bindGateIndex(gate, ix)
	if t := builtin.NewCodeSearch(ix, gate); t != nil {
		reg.Add(t)
	}
	addCommitSearch(ctx, reg, root, cfg, keys, proxy, stderr)
	// Sin la cancelación heredada: el escaneo inicial sobrevive al ensamblaje,
	// que termina mucho antes de que el índice esté al día.
	bg := context.WithoutCancel(ctx)
	syncCodeSearch(bg, ix, stderr)
	if cfg.Watch && watcher != nil {
		watcher.Index = ix
		go watcher.Run(bg)
	}
	return ix
}

// OpenCodeSearchIndex arma el índice desde la configuración: cliente del
// proveedor, almacén de vectores y estado incremental.
func OpenCodeSearchIndex(ctx context.Context, root string, cfg config.CodeSearchConfig, keys *codesearch.Keyring, proxy netclient.ProxySpec) (*codesearch.Index, error) {
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
	if err != nil {
		client = &http.Client{}
	}
	client.Timeout = codeSearchHTTPTimeout

	voyage := &codesearch.Voyage{
		Keys:        keys,
		EmbedModel:  cfg.Model,
		RerankModel: cfg.RerankModel,
		Dimensions:  cfg.Dimensions,
		BaseURL:     cfg.BaseURL,
		HTTP:        client,
	}
	// El índice se guarda bajo la identidad del workspace y no bajo su ruta,
	// para que mover la carpeta no obligue a reindexar y volver a pagarlo. La
	// misma identidad separa proyectos dentro de una base compartida.
	ws := codesearch.IdentifyWorkspaceIn(root, cfg.ContainerPaths())
	dir := filepath.Join(config.CodeSearchIndexDir(), ws.ID)

	store, err := OpenCodeSearchStore(ctx, dir, ws.ID, ws.Name, cfg)
	if err != nil {
		return nil, err
	}
	state, err := codesearch.LoadState(dir)
	if err != nil {
		return nil, fmt.Errorf("load code index state: %w", err)
	}

	var reranker codesearch.Reranker
	if cfg.RerankModel != "" {
		reranker = voyage
	}
	return codesearch.NewIndex(root, store, state, voyage, reranker), nil
}

// syncCodeSearch pone al día el índice en segundo plano. Va aparte del arranque
// porque un escaneo inicial puede tardar minutos y bloquear la sesión sería
// peor que buscar con el índice a medias.
func syncCodeSearch(ctx context.Context, ix *codesearch.Index, stderr io.Writer) {
	if ix == nil {
		return
	}
	go func() {
		st, err := ix.Sync(ctx, nil)
		if errors.Is(err, codesearch.ErrQuotaExhausted) {
			// Sin cuota el índice deja de crecer, y eso hay que decirlo fuerte:
			// si no, la búsqueda simplemente empeora sin explicación.
			fmt.Fprintf(stderr, "code_search: se agotó la cuota del proveedor; el índice quedó incompleto. %v\n", err)
			return
		}
		if err != nil {
			fmt.Fprintf(stderr, "code_search index sync: %v\n", err)
			return
		}
		if st.Embedded > 0 || st.Removed > 0 {
			fmt.Fprintf(stderr, "code_search: indexed %d files, removed %d, %d chunks total\n", st.Embedded, st.Removed, st.Chunks)
		}
	}()
}

// indexDirForTest expone dónde queda el índice de un workspace, para poder
// verificar que vive fuera del proyecto y bajo su identidad.
func indexDirForTest(root string) string {
	return filepath.Join(config.CodeSearchIndexDir(), codesearch.IdentifyWorkspace(root).ID)
}

// codeSearchAvailable predice si la herramienta se va a registrar, sin
// construir el índice, para decidir la guía del prompt antes del ensamblaje.
func codeSearchAvailable(cfg config.CodeSearchConfig, root string) bool {
	return cfg.Enabled && root != "" && os.Getenv(cfg.Normalized().APIKeyEnv) != ""
}

// publishCodeSearchStatus conecta el avance del índice al controlador, para que
// los tres frontends puedan pintarlo sin conocer el motor.
func publishCodeSearchStatus(ctrl *control.Controller, ix *codesearch.Index) {
	if ctrl == nil || ix == nil {
		return
	}
	ctrl.SetIndexStatusFunc(func() control.IndexStatus {
		s := ix.Status()
		return control.IndexStatus{
			Phase: string(s.Phase), Done: s.Done, Total: s.Total,
			Chunks: s.Chunks, First: s.First, Err: s.Err,
		}
	})
}

// newSearchGate arma la fricción de grep desde la configuración. Devuelve nil
// cuando está apagada, para que grep quede byte-idéntico al de siempre.
func newSearchGate(cfg config.CodeSearchConfig) *builtin.SearchGate {
	cfg = cfg.Normalized()
	if !cfg.Enabled || cfg.GrepFriction == config.FrictionOff {
		return nil
	}
	return &builtin.SearchGate{
		Mode:  builtin.SearchFriction(cfg.GrepFriction),
		Limit: cfg.GrepFrictionLimit,
	}
}

// bindGateIndex le enseña al gate a consultar el índice, para que se desactive
// solo cuando no haya a dónde mandar al modelo.
func bindGateIndex(gate *builtin.SearchGate, ix *codesearch.Index) {
	if gate == nil || ix == nil {
		return
	}
	gate.Usable = func() bool { _, ok := ix.Ready(); return ok }
}

// OpenCodeSearchStore elige dónde viven los vectores. El backend remoto no cae
// al local en silencio si falla: quien configuró Postgres espera que su índice
// viaje entre máquinas, y un local improvisado se vería igual de bien mientras
// no cumple eso.
func OpenCodeSearchStore(ctx context.Context, dir, workspaceID, name string, cfg config.CodeSearchConfig) (codesearch.VectorStore, error) {
	if cfg.Backend != config.BackendPostgres {
		store, err := codesearch.OpenLocalStore(dir, cfg.Model, cfg.Dimensions)
		if err != nil {
			return nil, fmt.Errorf("open code index: %w", err)
		}
		return store, nil
	}
	dsn := os.Getenv(cfg.PostgresURLEnv)
	if dsn == "" {
		return nil, fmt.Errorf("code index backend postgres: %s is not set", cfg.PostgresURLEnv)
	}
	store, err := codesearch.OpenPostgresStore(ctx, dsn, workspaceID, name, cfg.Model, cfg.Dimensions)
	if err != nil {
		return nil, fmt.Errorf("open code index: %w", err)
	}
	return store, nil
}

// newCodeSearchWatcher arma el vigilante antes que las herramientas, para que
// puedan avisarle de cada escritura desde el primer turno. Arranca sin índice y
// lo recibe cuando existe; hasta entonces un aviso simplemente no hace nada.
func newCodeSearchWatcher(cfg config.CodeSearchConfig) *codesearch.Watcher {
	cfg = cfg.Normalized()
	if !cfg.Enabled || !cfg.Watch {
		return nil
	}
	return &codesearch.Watcher{}
}

// addCommitSearch registra la búsqueda sobre la historia y la sincroniza en
// segundo plano. Comparte credencial y proveedor con el índice de código, pero
// usa un almacén propio: son dos búsquedas distintas y mezclarlas haría que una
// consulta sobre código devolviera commits.
func addCommitSearch(ctx context.Context, reg *tool.Registry, root string, cfg config.CodeSearchConfig, keys *codesearch.Keyring, proxy netclient.ProxySpec, stderr io.Writer) {
	if !cfg.Commits {
		return
	}
	ix, err := OpenCommitIndex(ctx, root, cfg, keys, proxy)
	if err != nil {
		fmt.Fprintf(stderr, "git_commit_search disabled: %v\n", err)
		return
	}
	if t := builtin.NewCommitSearch(ix); t != nil {
		reg.Add(t)
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		if _, err := ix.Sync(bg, nil); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(stderr, "commit index: %v\n", err)
		}
	}()
}

// OpenCommitIndex arma el índice de historia. El almacén va bajo una identidad
// derivada de la del workspace para no pisar el índice de código.
func OpenCommitIndex(ctx context.Context, root string, cfg config.CodeSearchConfig, keys *codesearch.Keyring, proxy netclient.ProxySpec) (*codesearch.CommitIndex, error) {
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{})
	if err != nil {
		client = &http.Client{}
	}
	client.Timeout = codeSearchHTTPTimeout

	voyage := &codesearch.Voyage{
		Keys:        keys,
		EmbedModel:  cfg.Model,
		RerankModel: cfg.RerankModel,
		Dimensions:  cfg.Dimensions,
		BaseURL:     cfg.BaseURL,
		HTTP:        client,
	}
	ws := codesearch.IdentifyWorkspaceIn(root, cfg.ContainerPaths())
	id := ws.ID + commitWorkspaceSuffix
	dir := filepath.Join(config.CodeSearchIndexDir(), id)
	store, err := OpenCodeSearchStore(ctx, dir, id, ws.Name, cfg)
	if err != nil {
		return nil, err
	}
	var reranker codesearch.Reranker
	if cfg.RerankModel != "" {
		reranker = voyage
	}
	return codesearch.NewCommitIndex(root, store, voyage, reranker, cfg.CommitLimit), nil
}

// commitWorkspaceSuffix separa el índice de historia del de código dentro del
// mismo almacén compartido.
const commitWorkspaceSuffix = "-commits"

// CodeSearchKeyring reúne las credenciales del proveedor desde el entorno. Se
// admiten varias en la misma variable separadas por coma, y variables numeradas
// a partir de la segunda: así, cuando una cuenta se queda sin cuota, basta con
// dar de alta otra llave sin tocar la configuración.
func CodeSearchKeyring(envName string) *codesearch.Keyring {
	if envName == "" {
		return codesearch.NewKeyring()
	}
	keys := codesearch.SplitKeys(os.Getenv(envName))
	for n := 2; ; n++ {
		raw := os.Getenv(fmt.Sprintf("%s_%d", envName, n))
		if strings.TrimSpace(raw) == "" {
			break
		}
		keys = append(keys, codesearch.SplitKeys(raw)...)
	}
	return codesearch.NewKeyring(keys...)
}
