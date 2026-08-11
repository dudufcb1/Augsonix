package codesearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitFor espera a que cond se cumpla, para no depender de tiempos fijos.
func waitFor(t *testing.T, limit time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// startWatcher arranca el sondeo y garantiza que se detenga antes de que el
// test limpie su directorio; si no, el watcher sigue escribiendo mientras se
// borra y la limpieza falla.
func startWatcher(t *testing.T, ix *Index, interval, quiet time.Duration) *Watcher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{Index: ix, Interval: interval, Quiet: quiet}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return w
}

func TestWatcherIndexesNewFileAfterNotify(t *testing.T) {
	// El caso que importa: el agente escribe un archivo y el índice se entera
	// sin esperar al reloj. Es de donde viene casi todo el cambio en una sesión.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	_, before := ix.store.Stats()

	w := startWatcher(t, ix, time.Hour, 20*time.Millisecond)

	writeFile(t, root, "b.go", body("beta"))
	w.Notify()

	if !waitFor(t, 3*time.Second, func() bool {
		_, now := ix.store.Stats()
		return now > before
	}) {
		t.Error("el archivo nuevo no se indexó tras avisar")
	}
}

func TestWatcherPicksUpExternalChanges(t *testing.T) {
	// Un cambio hecho fuera del agente (el editor, un git checkout) no dispara
	// Notify, así que el sondeo tiene que atraparlo igual.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	_, before := ix.store.Stats()

	startWatcher(t, ix, 30*time.Millisecond, 20*time.Millisecond)

	writeFile(t, root, "externo.go", body("desde el editor"))

	if !waitFor(t, 3*time.Second, func() bool {
		_, now := ix.store.Stats()
		return now > before
	}) {
		t.Error("el sondeo no atrapó un cambio externo")
	}
}

func TestWatcherWaitsForTreeToSettle(t *testing.T) {
	// Un git checkout toca cientos de archivos en ráfaga. Sin el periodo de
	// calma se reindexaría a media operación y habría que rehacerlo enseguida.
	ix, root, emb := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	callsBefore := emb.callCount()

	startWatcher(t, ix, 10*time.Millisecond, 400*time.Millisecond)

	// Ráfaga: mientras siga escribiendo, no debe indexar.
	for i := range 6 {
		writeFile(t, root, filepath.Join("burst", string(rune('a'+i))+".go"), body("burst"))
		time.Sleep(60 * time.Millisecond)
	}
	if emb.callCount() != callsBefore {
		t.Errorf("indexó a media ráfaga: %d llamadas nuevas", emb.callCount()-callsBefore)
	}

	if !waitFor(t, 3*time.Second, func() bool { return emb.callCount() > callsBefore }) {
		t.Error("no indexó una vez que el árbol se calmó")
	}
}

func TestWatcherIgnoresUnchangedTree(t *testing.T) {
	// Con el índice al día, sondear no debe embeber nada: si lo hiciera, cada
	// sesión abierta gastaría cuota sin que nadie tocara el código.
	ix, root, emb := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	callsBefore := emb.callCount()

	startWatcher(t, ix, 10*time.Millisecond, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)

	if emb.callCount() != callsBefore {
		t.Errorf("el sondeo embebió %d veces sin cambios en el árbol", emb.callCount()-callsBefore)
	}
}

func TestWatcherStopsWithContext(t *testing.T) {
	// Al cerrar la sesión el sondeo tiene que parar, o la goroutine queda viva
	// hasta que muera el proceso.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w := &Watcher{Index: ix, Interval: 10 * time.Millisecond, Quiet: 10 * time.Millisecond}
	go func() { w.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("el watcher siguió corriendo tras cancelar el contexto")
	}
}

func TestWatcherNotifyIsSafeBeforeRun(t *testing.T) {
	// Notify puede llegar antes de que el sondeo arranque, o incluso sin él.
	// No debe bloquear ni reventar por un canal sin inicializar.
	ix, _, _ := newTestIndex(t)
	w := &Watcher{Index: ix}
	done := make(chan struct{})
	go func() { w.Notify(); w.Notify(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Notify se bloqueó sin un sondeo corriendo")
	}
}

func TestWatcherDetectsDeletedFile(t *testing.T) {
	// Borrar un archivo también es un cambio: si no se atrapa, la búsqueda
	// seguiría devolviendo código que ya no existe en disco.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	writeFile(t, root, "b.go", body("beta"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	startWatcher(t, ix, 20*time.Millisecond, 20*time.Millisecond)

	if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !ix.store.Has("b.go") }) {
		t.Error("el archivo borrado siguió en el índice")
	}
}
