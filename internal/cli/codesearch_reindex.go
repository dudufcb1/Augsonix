package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/codesearch"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

// codeSearchReindex construye el índice hasta terminar, mostrando el avance.
// Existe porque el indexado normal corre en segundo plano y solo vive mientras
// dure la sesión: en un repositorio grande eso obliga a dejar reasonix abierto
// sin saber cuánto falta. Aquí el prompt vuelve cuando de verdad terminó.
func codeSearchReindex(cfg config.CodeSearchConfig, root string, force bool) int {
	if !cfg.Enabled {
		fmt.Fprintln(os.Stderr, "codesearch: apagado — activa [codesearch] enabled en reasonix.toml")
		return 1
	}
	key := os.Getenv(cfg.APIKeyEnv)
	if key == "" {
		fmt.Fprintf(os.Stderr, "codesearch: %s no está definida\n", cfg.APIKeyEnv)
		return 1
	}

	// Ctrl+C corta pero no tira lo hecho: lo embebido ya quedó guardado con su
	// hash, y el siguiente intento retoma desde ahí.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ix, err := boot.OpenCodeSearchIndex(ctx, root, cfg, key, netclient.ProxySpec{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	ws := codesearch.IdentifyWorkspace(root)
	if force {
		fmt.Printf("borrando el índice de %s antes de reconstruirlo…\n", ws.Name)
		if code := codeSearchClear(cfg, root); code != 0 {
			return code
		}
	}
	fmt.Printf("indexando %s\n", ws.Name)

	start := time.Now()
	bar := &progressBar{out: os.Stdout, start: start}
	st, err := ix.Sync(ctx, bar.update)
	bar.clear()

	if ctx.Err() != nil {
		fmt.Printf("\ninterrumpido tras %s. Lo indexado quedó guardado; vuelve a correrlo para continuar.\n",
			time.Since(start).Round(time.Second))
		return 130
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "codesearch: %v\n", err)
		return 1
	}
	fmt.Printf("listo en %s · %d archivos revisados, %d embebidos, %d sin cambios, %d fragmentos\n",
		time.Since(start).Round(time.Second), st.Scanned, st.Embedded, st.Unchanged, st.Chunks)
	if used := tokensUsed(ix); used > 0 {
		fmt.Printf("consumo    %s tokens del proveedor\n", humanCount(used))
	}
	if cfg.Commits {
		if code := reindexCommits(ctx, cfg, root, key); code != 0 {
			return code
		}
	}
	return 0
}

// reindexCommits construye el índice de la historia después del de código. Va
// aquí y no en un comando aparte porque quien reindexa un proyecto quiere las
// dos mitades al día, no una.
func reindexCommits(ctx context.Context, cfg config.CodeSearchConfig, root, key string) int {
	ix, err := boot.OpenCommitIndex(ctx, root, cfg, key, netclient.ProxySpec{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "commits: %v\n", err)
		return 1
	}
	fmt.Println("indexando la historia")
	start := time.Now()
	bar := &progressBar{out: os.Stdout, start: start}
	st, err := ix.Sync(ctx, func(s codesearch.CommitStats) {
		bar.update(codesearch.Progress{Done: s.Embedded, Total: s.Scanned, Embedded: s.Embedded})
	})
	bar.clear()

	if ctx.Err() != nil {
		fmt.Printf("\ninterrumpido tras %s. Los commits embebidos quedaron guardados.\n",
			time.Since(start).Round(time.Second))
		return 130
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "commits: %v\n", err)
		return 1
	}
	fmt.Printf("listo en %s · %d commits revisados, %d embebidos, %d sin cambios, %d retirados\n",
		time.Since(start).Round(time.Second), st.Scanned, st.Embedded, st.Unchanged, st.Removed)
	return 0
}

// tokensUsed pregunta al proveedor cuánto cobró, si sabe decirlo. Se resuelve
// por interfaz para no atar la CLI a un proveedor concreto.
func tokensUsed(ix *codesearch.Index) int64 {
	if c, ok := ix.Embedder().(interface{ TokensUsed() int64 }); ok {
		return c.TokensUsed()
	}
	return 0
}

// progressBar pinta el avance en una sola línea que se reescribe. Sin esto la
// espera es a ciegas, que es justo lo que hace insoportable un indexado largo.
type progressBar struct {
	// out se inyecta para poder verificar lo que se pinta sin ensuciar la
	// salida de los tests con barras a medio dibujar.
	out   io.Writer
	start time.Time
	width int
}

func (b *progressBar) update(p codesearch.Progress) {
	if p.Total <= 0 {
		return
	}
	pct := p.Done * 100 / p.Total
	filled := pct * 20 / 100
	line := fmt.Sprintf("  [%s%s] %d/%d (%d%%) · %s · %d embebidos",
		strings.Repeat("█", filled), strings.Repeat("░", 20-filled),
		p.Done, p.Total, pct, time.Since(b.start).Round(time.Second), p.Embedded)
	b.width = max(b.width, len(line))
	fmt.Fprintf(b.writer(), "\r%-*s", b.width, line)
}

func (b *progressBar) clear() {
	if b.width > 0 {
		fmt.Fprintf(b.writer(), "\r%*s\r", b.width, "")
	}
}

func (b *progressBar) writer() io.Writer {
	if b.out == nil {
		return os.Stdout
	}
	return b.out
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
