package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"reasonix/internal/claudesessions"
	"reasonix/internal/config"
	"reasonix/internal/schedulable"
	"reasonix/internal/scheduler"
)

// runScheduler serves the local agenda panel and wakes authorized sessions when
// their tasks come due. It loads no session, so it can stay up cheaply while
// everything else is closed.
func runScheduler(args []string) int {
	fs := newFlagSet("agenda")
	addr := fs.String("addr", "127.0.0.1:8788", "listen address")
	once := fs.Bool("once", false, "run whatever is due and exit, without serving the panel")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	reg, err := schedulable.Open(config.SchedulableDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenda:", err)
		return 1
	}
	runner := &scheduler.Runner{Registry: reg}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		ran, err := runner.RunDue(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agenda:", err)
			return 1
		}
		fmt.Printf("agenda: %d encargo(s) ejecutado(s)\n", ran)
		return 0
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenda:", err)
		return 1
	}
	go runner.Loop(ctx)

	srv := &http.Server{Handler: scheduler.NewServer(reg, runner,
		claudesessions.DiscoverHomes(userHomeDir()),
		filepath.Join(config.ReasonixHomeDir(), "config.toml"),
		filepath.Join(userHomeDir(), ".claude.json"),
		scheduler.LauncherSource{
			Dir:      filepath.Join(userHomeDir(), ".local", "share", "applications"),
			MainHome: filepath.Join(userHomeDir(), ".claude"),
		},
	).Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	fmt.Printf("agenda: http://%s\n", ln.Addr())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "agenda:", err)
		return 1
	}
	return 0
}

// userHomeDir localiza el home del usuario para descubrir los CLAUDE_CONFIG_DIR.
func userHomeDir() string {
	if dir, err := os.UserHomeDir(); err == nil {
		return dir
	}
	return os.Getenv("HOME")
}
