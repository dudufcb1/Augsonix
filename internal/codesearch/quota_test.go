package codesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestKeyUnusableRecognizesPaymentRequired(t *testing.T) {
	// El 402 es inequívoco: no hay con qué pagar el trabajo.
	if !keyUnusable(http.StatusPaymentRequired, "") {
		t.Error("no se reconoció un 402 como credencial sin margen")
	}
}

func TestKeyUnusableRecognizesRejectedCredential(t *testing.T) {
	// Una llave revocada o mal copiada no mejora reintentando: hay que probar
	// con otra. Voyage devuelve 401 para eso.
	if !keyUnusable(http.StatusUnauthorized, "Provided API key is invalid.") {
		t.Error("no se reconoció un 401 como credencial inservible")
	}
}

func TestKeyUnusableRecognizesWordings(t *testing.T) {
	// El proveedor no promete un código fijo, así que también se leen las
	// frases que dicen explícitamente que se acabó.
	for _, msg := range []string{
		"Insufficient credits for this request",
		"Your free token quota is exhausted",
		"You have exceeded your monthly allowance",
		"Account is out of credit",
	} {
		if !keyUnusable(http.StatusBadRequest, msg) {
			t.Errorf("no se reconoció como cuota agotada: %q", msg)
		}
	}
}

func TestKeyUnusableIgnoresRateLimitNotice(t *testing.T) {
	// Este es el caso que importa no confundir: el aviso de agregar método de
	// pago acompaña al límite de tasa reducido, que sí es reintentable. Tratarlo
	// como cuota agotada daría el índice por muerto cuando solo había que esperar.
	notice := "You have not yet added your payment method in the billing page and will have " +
		"reduced rate limits of 3 RPM and 10K TPM. To unlock our standard rate limits, please " +
		"add a payment method in the billing page."
	if keyUnusable(http.StatusTooManyRequests, notice) {
		t.Error("el aviso de límite de tasa se confundió con cuota agotada")
	}
}

func TestVoyageSurfacesQuotaAsTypedError(t *testing.T) {
	// El error tiene que llegar identificable hasta arriba: de eso depende que
	// la interfaz pueda decir "se acabó la cuota" en vez de un fallo genérico.
	calls := 0
	v := voyageServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]string{"detail": "insufficient credits"})
	})

	_, err := v.Embed(context.Background(), []string{"a"}, KindDocument)
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Errorf("el error no llegó como ErrQuotaExhausted: %v", err)
	}
	if calls != 1 {
		t.Errorf("hubo %d llamadas; reintentar sin cuota solo retrasa el aviso", calls)
	}
}

func TestSyncStopsAndReportsQuota(t *testing.T) {
	// Sin cuota no tiene sentido seguir archivo por archivo acumulando el mismo
	// error, y el estado debe quedar en la fase que la interfaz sabe explicar.
	ix, root, _ := newTestIndex(t)
	for _, n := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, root, n, body(n))
	}
	ix.embedder = &quotaEmbedder{}

	_, err := ix.Sync(context.Background(), nil)
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("Sync no propagó la cuota agotada: %v", err)
	}
	if got := ix.Status().Phase; got != PhaseQuota {
		t.Errorf("Phase = %q, esperaba %q", got, PhaseQuota)
	}
	if ix.Status().Err == nil {
		t.Error("quedó en fase de cuota pero sin error que mostrar")
	}
}

// quotaEmbedder simula un proveedor sin saldo. El contador va atómico porque
// durante un escaneo lo llaman varios trabajadores a la vez.
type quotaEmbedder struct{ calls atomic.Int32 }

func (q *quotaEmbedder) Embed(context.Context, []string, InputKind) ([][]int8, error) {
	q.calls.Add(1)
	return nil, ErrQuotaExhausted
}
func (q *quotaEmbedder) Dims() int     { return 8 }
func (q *quotaEmbedder) Model() string { return "fake" }

func TestSyncStopsTheOtherWorkersWhenQuotaRunsOut(t *testing.T) {
	// El primero que descubre que no hay cuota tiene que frenar a los demás.
	// Si cada trabajador siguiera con su cola, un workspace grande dispararía
	// miles de peticiones contra una cuenta muerta.
	ix, root, _ := newTestIndex(t)
	const files = 400
	for i := range files {
		writeFile(t, root, fmt.Sprintf("archivo%03d.go", i), body("contenido"))
	}
	emb := &quotaEmbedder{}
	ix.embedder = emb

	if _, err := ix.Sync(context.Background(), nil); !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("Sync no propagó la cuota agotada: %v", err)
	}
	// Los trabajadores ya en vuelo terminan lo suyo, así que se tolera un
	// puñado; lo que no puede es haber recorrido el workspace entero.
	if got := int(emb.calls.Load()); got > syncWorkers*2 {
		t.Errorf("se intentaron %d archivos tras quedarse sin cuota; se esperaban a lo mucho %d",
			got, syncWorkers*2)
	}
}
