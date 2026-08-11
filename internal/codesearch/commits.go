package codesearch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// commitDiffLimit recorta el diff que acompaña al mensaje. Lo que responde
	// una búsqueda es la intención, que está en el mensaje; el diff solo aporta
	// el vocabulario técnico —nombres de funciones y archivos— y para eso basta
	// con el principio.
	commitDiffLimit = 4000
	// commitGitTimeout acota cada invocación a git: un repositorio corrupto o
	// un hook colgado no puede dejar la indexación esperando para siempre.
	commitGitTimeout = 30 * time.Second
)

// Commit es un commit listo para indexar: lo que escribió el autor, lo que tocó
// y un recorte de lo que cambió.
type Commit struct {
	Hash    string
	Date    string
	Author  string
	Subject string
	Body    string
	Files   []string
	Diff    string
}

// Document arma el texto que se embebe. El mensaje va primero porque ya dice la
// intención del cambio, que es lo que alguien busca; el diff va al final como
// contexto técnico.
func (c Commit) Document() string {
	var b strings.Builder
	b.WriteString(c.Subject)
	b.WriteString("\n")
	if c.Body != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Body)
	}
	fmt.Fprintf(&b, "\n%s · %s\n", c.Date, c.Author)
	if len(c.Files) > 0 {
		fmt.Fprintf(&b, "Archivos: %s\n", strings.Join(c.Files, ", "))
	}
	if c.Diff != "" {
		fmt.Fprintf(&b, "\n%s", c.Diff)
	}
	return b.String()
}

// Summary describe el commit en una línea para un resultado de búsqueda.
func (c Commit) Summary() string {
	return fmt.Sprintf("%s %s  %s", shortHash(c.Hash), c.Date, c.Subject)
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// gitAvailable reporta si hay un repositorio utilizable en root. Sin esto, cada
// comando fallaría por separado y la indexación reportaría errores en vez de
// simplemente no tener commits que ofrecer.
func gitAvailable(ctx context.Context, root string) bool {
	out, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// ExtractCommits lee los últimos max commits de la rama actual. Devuelve los
// más recientes primero, que son los que más se buscan.
func ExtractCommits(ctx context.Context, root string, max int) ([]Commit, error) {
	if !gitAvailable(ctx, root) {
		return nil, nil
	}
	hashes, err := commitHashes(ctx, root, max)
	if err != nil {
		return nil, err
	}
	out := make([]Commit, 0, len(hashes))
	for _, h := range hashes {
		c, err := describeCommit(ctx, root, h)
		if err != nil {
			continue // un commit ilegible no debe tumbar la indexación entera
		}
		out = append(out, c)
	}
	return out, nil
}

func commitHashes(ctx context.Context, root string, max int) ([]string, error) {
	out, err := git(ctx, root, "log", fmt.Sprintf("-%d", max), "--format=%H")
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	return hashes, nil
}

// commitFieldSep separa campos dentro de un registro. Se usa un carácter de
// control porque cualquier separador imprimible puede aparecer en un mensaje.
const commitFieldSep = "\x1f"

func describeCommit(ctx context.Context, root, hash string) (Commit, error) {
	format := strings.Join([]string{"%H", "%ad", "%an", "%s", "%b"}, commitFieldSep)
	out, err := git(ctx, root, "show", "--no-patch", "--date=short", "--format="+format, hash)
	if err != nil {
		return Commit{}, err
	}
	parts := strings.SplitN(strings.TrimRight(out, "\n"), commitFieldSep, 5)
	if len(parts) < 5 {
		return Commit{}, fmt.Errorf("codesearch: formato inesperado para %s", shortHash(hash))
	}
	c := Commit{
		Hash: parts[0], Date: parts[1], Author: parts[2],
		Subject: strings.TrimSpace(parts[3]), Body: strings.TrimSpace(parts[4]),
	}
	c.Files = commitFiles(ctx, root, hash)
	c.Diff = commitDiff(ctx, root, hash)
	return c, nil
}

func commitFiles(ctx context.Context, root, hash string) []string {
	out, err := git(ctx, root, "show", "--name-only", "--format=", hash)
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// commitDiff trae el cambio con una sola línea de contexto: el contexto extra
// engorda el texto sin aportar señal, porque es código que el commit no tocó.
func commitDiff(ctx context.Context, root, hash string) string {
	out, err := git(ctx, root, "show", "--format=", "--unified=1", hash)
	if err != nil {
		return ""
	}
	if len(out) > commitDiffLimit {
		return out[:commitDiffLimit]
	}
	return out
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commitGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
