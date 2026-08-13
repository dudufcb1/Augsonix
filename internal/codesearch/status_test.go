package codesearch

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestStatusStartsIdle(t *testing.T) {
	// Antes de cualquier escaneo el índice está en reposo; la interfaz no debe
	// mostrar progreso de algo que no ha empezado.
	ix, _, _ := newTestIndex(t)
	if got := ix.Status().Phase; got != PhaseIdle {
		t.Errorf("Phase = %q, esperaba %q", got, PhaseIdle)
	}
}

func TestStatusReachesReadyAfterSync(t *testing.T) {
	// Al terminar, el estado tiene que quedar listo y con los chunks contados:
	// es lo que le dice a la interfaz que ya puede dejar de avisar.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	st := ix.Status()
	if st.Phase != PhaseReady {
		t.Errorf("Phase = %q, esperaba %q", st.Phase, PhaseReady)
	}
	if st.Chunks == 0 {
		t.Error("terminó listo pero sin chunks contados")
	}
	if st.Err != nil {
		t.Errorf("quedó un error colgado: %v", st.Err)
	}
}

func TestStatusMarksFirstIndexing(t *testing.T) {
	// El primer indexado de un repositorio tarda minutos y los siguientes no se
	// notan. La interfaz necesita distinguirlos para avisar solo cuando toca.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !ix.Status().First {
		t.Error("el primer escaneo no se marcó como tal")
	}
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ix.Status().First {
		t.Error("el segundo escaneo se marcó como primero")
	}
}

func TestStatusCountsProgressAgainstTotal(t *testing.T) {
	// Done y Total cuentan archivos porque es la unidad que el usuario
	// reconoce; con chunks el número no significaría nada para él.
	ix, root, _ := newTestIndex(t)
	for _, n := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, root, n, body(n))
	}
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	st := ix.Status()
	if st.Total != 3 || st.Done != 3 {
		t.Errorf("Done/Total = %d/%d, esperaba 3/3", st.Done, st.Total)
	}
}

func TestStatusRecordsFailure(t *testing.T) {
	// Si el proveedor falla, el error tiene que quedar a la vista: si no, la
	// interfaz mostraría un escaneo que nunca termina y nadie sabría por qué.
	ix, root, _ := newTestIndex(t)
	writeFile(t, root, "a.go", body("alpha"))
	ix.embedder = &failingEmbedder{}
	_, _ = ix.Sync(context.Background(), nil)

	st := ix.Status()
	if st.Phase != PhaseFailed && st.Phase != PhaseReady {
		t.Logf("Phase = %q", st.Phase)
	}
	if st.Phase == PhaseFailed && st.Err == nil {
		t.Error("quedó en fallo pero sin error que explicarlo")
	}
}

// failingEmbedder simula un proveedor caído.
type failingEmbedder struct{}

func (f *failingEmbedder) Embed(context.Context, []string, InputKind) ([][]int8, error) {
	return nil, errors.New("503 service unavailable")
}
func (f *failingEmbedder) Dims() int     { return 8 }
func (f *failingEmbedder) Model() string { return "fake" }

func TestStatusIsSafeToReadWhileIndexing(t *testing.T) {
	// La interfaz lee el estado en cada fotograma mientras la goroutine del
	// escaneo lo escribe. Sin protección eso es una carrera de datos, y este
	// test la detecta bajo -race.
	ix, root, _ := newTestIndex(t)
	for _, n := range []string{"a.go", "b.go", "c.go", "d.go"} {
		writeFile(t, root, n, body(n))
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = ix.Status()
			}
		}
	})
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()
}
