package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/codesearch"
	"reasonix/internal/tool"
)

// CodeSearchIndex es lo que la tool necesita del índice semántico. Interfaz y
// no el tipo concreto para que el registro de tools no dependa del backend de
// vectores que haya detrás.
type CodeSearchIndex interface {
	Search(ctx context.Context, query string, opts codesearch.SearchOptions) ([]codesearch.Result, error)
	// Ready reporta cuántos chunks hay indexados y si se puede buscar.
	Ready() (chunks int, ok bool)
}

type codeSearch struct {
	index CodeSearchIndex
}

// NewCodeSearch liga la tool a un índice. Devuelve nil cuando no hay índice, y
// entonces la tool no se registra: una tool inútil sigue costando tokens de
// prefijo en cada turno de cada sesión.
func NewCodeSearch(index CodeSearchIndex) tool.Tool {
	if index == nil {
		return nil
	}
	return codeSearch{index: index}
}

func (codeSearch) Name() string { return "code_search" }

// La descripción es deliberadamente corta: viaja en el prefijo del prompt en
// cada turno de cada sesión, así que cada palabra se paga muchas veces.
func (codeSearch) Description() string {
	return "Semantic search over this workspace's code. Takes a natural-language description of what you need and returns the most relevant code with file path and line range. Matches on meaning, so it finds code whose identifiers you cannot guess, across languages, and when the query wording appears nowhere in the source — cases where grep returns nothing. Prefer it to locate code before reading or editing; use grep when you already know the exact string."
}

func (codeSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "request":{"type":"string","description":"What you need to find, in plain language. Ask for everything relevant in one call rather than several narrow ones."},
  "path":{"type":"string","description":"Optional directory prefix to restrict the search to, such as \"internal/agent/\"."},
  "limit":{"type":"integer","description":"Maximum results (default 10, max 40).","minimum":1}
},
"required":["request"]
}`)
}

func (codeSearch) ReadOnly() bool { return true }

const (
	codeSearchDefaultLimit = 10
	codeSearchMaxLimit     = 40
)

type codeSearchArgs struct {
	Request string `json:"request"`
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
}

func (c codeSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p codeSearchArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Request) == "" {
		return "", fmt.Errorf("request is required")
	}
	if c.index == nil {
		return "", fmt.Errorf("code_search is not configured: set [codesearch] in reasonix.toml")
	}
	// Un índice vacío no es un error: es un índice que aún no se construye. El
	// modelo necesita saberlo para no concluir que el código no existe.
	if chunks, ok := c.index.Ready(); !ok {
		return fmt.Sprintf("The semantic index is empty (%d chunks); it may still be building. Use grep or glob for this lookup.", chunks), nil
	}
	if p.Limit <= 0 {
		p.Limit = codeSearchDefaultLimit
	}
	if p.Limit > codeSearchMaxLimit {
		p.Limit = codeSearchMaxLimit
	}

	results, err := c.index.Search(ctx, p.Request, codesearch.SearchOptions{
		Limit:      p.Limit,
		PathPrefix: p.Path,
	})
	if err != nil {
		return "", fmt.Errorf("code_search: %w", err)
	}
	return formatCodeSearch(results), nil
}

// formatCodeSearch arma el texto que ve el modelo. Cada bloque abre con
// path:inicio-fin para que pueda leer o editar sin volver a buscar el archivo.
func formatCodeSearch(results []codesearch.Result) string {
	if len(results) == 0 {
		return "No matches. Try different wording, or grep if you know the exact string."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d results, most relevant first.\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "\n%s:%d-%d (score %.2f)\n", r.Chunk.Path, r.Chunk.StartLine, r.Chunk.EndLine, r.Score)
		b.WriteString(r.Chunk.Content)
		if !strings.HasSuffix(r.Chunk.Content, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

var _ CodeSearchIndex = (*codesearch.Index)(nil)
