package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/codesearch"
	"reasonix/internal/tool"
)

// CommitSearchIndex es lo que la tool necesita del índice de historia.
type CommitSearchIndex interface {
	Search(ctx context.Context, query string, limit int) ([]codesearch.CommitResult, error)
	// Ready reporta cuántos commits hay indexados y si se puede buscar.
	Ready() (commits int, ok bool)
}

type commitSearch struct {
	index CommitSearchIndex
}

// NewCommitSearch liga la tool a un índice de historia. Devuelve nil sin
// índice: una tool que no puede responder sigue costando tokens de prefijo en
// cada turno de cada sesión.
func NewCommitSearch(index CommitSearchIndex) tool.Tool {
	if index == nil {
		return nil
	}
	return commitSearch{index: index}
}

func (commitSearch) Name() string { return "git_commit_search" }

// La descripción viaja en el prefijo del prompt en cada turno, así que cada
// palabra se paga muchas veces.
func (commitSearch) Description() string {
	return "Semantic search over this repository's commit history. Takes a natural-language description and returns the commits whose message and changes match it, most relevant first. Use it to find how a similar change was made before, why something is the way it is, or when a behavior was introduced. Returns commit hashes; run git show on one to see the diff."
}

func (commitSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "request":{"type":"string","description":"What you are looking for in the history, in plain language."},
  "limit":{"type":"integer","description":"Maximum results (default 10, max 25).","minimum":1}
},
"required":["request"]
}`)
}

func (commitSearch) ReadOnly() bool { return true }

const (
	commitSearchDefaultLimit = 10
	commitSearchMaxLimit     = 25
)

type commitSearchArgs struct {
	Request string `json:"request"`
	Limit   int    `json:"limit"`
}

func (g commitSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p commitSearchArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Request) == "" {
		return "", fmt.Errorf("request is required")
	}
	// Un índice vacío no es un error: puede estar construyéndose, o el
	// directorio puede no ser un repositorio. El modelo necesita saberlo para
	// no concluir que el cambio nunca ocurrió.
	if commits, ok := g.index.Ready(); !ok {
		return fmt.Sprintf("The commit index is empty (%d commits); it may still be building, or this is not a git repository. Use git log for this lookup.", commits), nil
	}
	if p.Limit <= 0 {
		p.Limit = commitSearchDefaultLimit
	}
	if p.Limit > commitSearchMaxLimit {
		p.Limit = commitSearchMaxLimit
	}
	results, err := g.index.Search(ctx, p.Request, p.Limit)
	if err != nil {
		return "", fmt.Errorf("git_commit_search: %w", err)
	}
	return formatCommitSearch(results), nil
}

// formatCommitSearch arma el texto que ve el modelo: el mensaje entero de cada
// commit, sin el diff, con el hash por delante para poder pedirlo a git.
func formatCommitSearch(results []codesearch.CommitResult) string {
	if len(results) == 0 {
		return "No matching commits. Try different wording, or git log if you know the exact string."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d commits, most relevant first.\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "\n%s (score %.2f)\n", r.Hash, r.Score)
		b.WriteString(r.Message())
		b.WriteString("\n")
	}
	return b.String()
}
