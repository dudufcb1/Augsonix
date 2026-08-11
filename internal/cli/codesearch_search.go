package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"reasonix/internal/boot"
	"reasonix/internal/codesearch"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

// searchDefaultLimit son los resultados que se muestran sin pedir otra cosa.
const searchDefaultLimit = 10

// codeSearchSearch consulta el índice desde la línea de comandos. Existe para
// que otras herramientas —otro agente, un script, un hook— puedan aprovechar un
// índice ya construido sin levantar la interfaz ni el agente.
func codeSearchSearch(cfg config.CodeSearchConfig, root string, args []string) int {
	opts, query, err := parseSearchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 2
	}
	if !cfg.Enabled {
		fmt.Fprintln(os.Stderr, "codesearch: apagado — activa [codesearch] enabled en reasonix.toml")
		return 1
	}
	keys := boot.CodeSearchKeyring(cfg.APIKeyEnv)
	if keys.Len() == 0 {
		fmt.Fprintf(os.Stderr, "codesearch: %s no está definida\n", cfg.APIKeyEnv)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.commits {
		return searchCommits(ctx, cfg, root, keys, query, opts)
	}
	return searchCode(ctx, cfg, root, keys, query, opts)
}

// searchOptions son las banderas del comando.
type searchOptions struct {
	limit   int
	path    string
	asJSON  bool
	commits bool
}

func parseSearchArgs(args []string) (searchOptions, string, error) {
	opts := searchOptions{limit: searchDefaultLimit}
	var terms []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			opts.asJSON = true
		case a == "--commits":
			opts.commits = true
		case a == "--limit" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, "", fmt.Errorf("--limit espera un número positivo, no %q", args[i])
			}
			opts.limit = n
		case a == "--path" && i+1 < len(args):
			i++
			opts.path = args[i]
		case strings.HasPrefix(a, "-"):
			return opts, "", fmt.Errorf("opción desconocida %q", a)
		default:
			terms = append(terms, a)
		}
	}
	query := strings.TrimSpace(strings.Join(terms, " "))
	if query == "" {
		return opts, "", fmt.Errorf("falta la consulta")
	}
	return opts, query, nil
}

func searchCode(ctx context.Context, cfg config.CodeSearchConfig, root string, keys *codesearch.Keyring, query string, opts searchOptions) int {
	ix, err := boot.OpenCodeSearchIndex(ctx, root, cfg, keys, netclient.ProxySpec{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	if chunks, ok := ix.Ready(); !ok {
		fmt.Fprintf(os.Stderr, "codesearch: este proyecto no tiene índice (%d fragmentos). Constrúyelo con: reasonix codesearch reindex\n", chunks)
		return 1
	}
	results, err := ix.Search(ctx, query, codesearch.SearchOptions{Limit: opts.limit, PathPrefix: opts.path})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	if opts.asJSON {
		return emitJSON(codeResultsJSON(results))
	}
	if len(results) == 0 {
		fmt.Println("sin resultados. Prueba con otras palabras, o grep si sabes la cadena exacta.")
		return 0
	}
	for _, r := range results {
		fmt.Printf("\n%s:%d-%d  (%.2f)\n", r.Chunk.Path, r.Chunk.StartLine, r.Chunk.EndLine, r.Score)
		fmt.Println(r.Chunk.Content)
	}
	return 0
}

func searchCommits(ctx context.Context, cfg config.CodeSearchConfig, root string, keys *codesearch.Keyring, query string, opts searchOptions) int {
	ix, err := boot.OpenCommitIndex(ctx, root, cfg, keys, netclient.ProxySpec{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	if commits, ok := ix.Ready(); !ok {
		fmt.Fprintf(os.Stderr, "codesearch: la historia no está indexada (%d commits). Activa commits en la configuración y corre: reasonix codesearch reindex\n", commits)
		return 1
	}
	results, err := ix.Search(ctx, query, opts.limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	if opts.asJSON {
		return emitJSON(commitResultsJSON(results))
	}
	if len(results) == 0 {
		fmt.Println("sin commits que coincidan. Prueba con otras palabras, o git log si sabes la cadena exacta.")
		return 0
	}
	for _, r := range results {
		fmt.Printf("\n%s  (%.2f)\n%s\n", r.Hash, r.Score, r.Message())
	}
	return 0
}

// searchHit es un resultado en JSON, para que otra herramienta lo consuma sin
// tener que interpretar el formato de pantalla.
type searchHit struct {
	Path    string  `json:"path"`
	Start   int     `json:"start_line,omitempty"`
	End     int     `json:"end_line,omitempty"`
	Score   float32 `json:"score"`
	Content string  `json:"content"`
}

func codeResultsJSON(results []codesearch.Result) []searchHit {
	out := make([]searchHit, 0, len(results))
	for _, r := range results {
		out = append(out, searchHit{
			Path: r.Chunk.Path, Start: r.Chunk.StartLine, End: r.Chunk.EndLine,
			Score: r.Score, Content: r.Chunk.Content,
		})
	}
	return out
}

func commitResultsJSON(results []codesearch.CommitResult) []searchHit {
	out := make([]searchHit, 0, len(results))
	for _, r := range results {
		out = append(out, searchHit{Path: r.Hash, Score: r.Score, Content: r.Message()})
	}
	return out
}

func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	return 0
}
