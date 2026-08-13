package codesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestKeyringDropsEmptyAndRepeatedKeys(t *testing.T) {
	// Una credencial repetida daría por buena una cuota que ya se agotó: al
	// retirar la primera copia, la segunda apuntaría a la misma cuenta muerta.
	k := NewKeyring("uno", "", "  ", "dos", "uno", "\tdos ")
	if k.Len() != 2 {
		t.Fatalf("quedaron %d credenciales, esperaba 2", k.Len())
	}
	got, _, ok := k.Current()
	if !ok || got != "uno" {
		t.Errorf("la primera credencial es %q (ok=%v)", got, ok)
	}
}

func TestKeyringMovesToTheNextWhenOneRunsOut(t *testing.T) {
	// El comportamiento central: al agotarse una entra la siguiente y el
	// trabajo sigue, sin que nadie tenga que intervenir.
	k := NewKeyring("a", "b", "c")
	_, slot, _ := k.Current()
	if more := k.Retire(slot); !more {
		t.Fatal("dijo que no quedaban credenciales habiendo dos")
	}
	got, _, ok := k.Current()
	if !ok || got != "b" {
		t.Errorf("credencial en uso = %q (ok=%v), esperaba \"b\"", got, ok)
	}
	if k.Alive() != 2 {
		t.Errorf("quedan %d vivas, esperaba 2", k.Alive())
	}
}

func TestKeyringReportsWhenNothingIsLeft(t *testing.T) {
	// Cuando ya no queda ninguna hay que decirlo, no seguir mandando peticiones
	// con una credencial muerta.
	k := NewKeyring("a", "b")
	for i := range 2 {
		_, slot, ok := k.Current()
		if !ok {
			t.Fatalf("se quedó sin credenciales en la vuelta %d", i)
		}
		k.Retire(slot)
	}
	if _, _, ok := k.Current(); ok {
		t.Error("sigue ofreciendo una credencial con todas agotadas")
	}
	if k.Alive() != 0 {
		t.Errorf("dice que quedan %d vivas", k.Alive())
	}
}

func TestKeyringIgnoresRetiringTheSameSlotTwice(t *testing.T) {
	// Dos peticiones en paralelo pueden fallar con la misma credencial. Si cada
	// una retirara una posición, se perdería una credencial sana por cada
	// choque.
	k := NewKeyring("a", "b", "c")
	k.Retire(0)
	k.Retire(0)
	k.Retire(0)
	if k.Alive() != 2 {
		t.Errorf("quedan %d vivas, esperaba 2", k.Alive())
	}
}

func TestKeyringWarnsOnceForEachSpentKey(t *testing.T) {
	// El usuario tiene que enterarse para dar de alta otra: si nadie avisa, se
	// queda sin margen sin saberlo.
	k := NewKeyring("a", "b")
	var avisos []string
	k.OnRetire(func(spent, alive, total int) {
		avisos = append(avisos, fmt.Sprintf("%d/%d quedan %d", spent, total, alive))
	})
	k.Retire(0)
	k.Retire(0) // repetida: no debe avisar de nuevo
	k.Retire(1)
	want := []string{"1/2 quedan 1", "2/2 quedan 0"}
	if len(avisos) != len(want) {
		t.Fatalf("avisos = %v, esperaba %v", avisos, want)
	}
	for i := range want {
		if avisos[i] != want[i] {
			t.Errorf("aviso %d = %q, esperaba %q", i, avisos[i], want[i])
		}
	}
}

func TestKeyringIsSafeUnderConcurrency(t *testing.T) {
	// El vigilante indexa mientras el usuario busca, así que dos goroutines
	// pueden pedir credencial y retirarla a la vez.
	k := NewKeyring("a", "b", "c", "d")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, slot, ok := k.Current(); ok {
				k.Retire(slot)
			}
		})
	}
	wg.Wait()
	if k.Alive() != 0 {
		t.Errorf("quedaron %d vivas tras retirarlas todas", k.Alive())
	}
}

func TestSplitKeysAcceptsTheUsualSeparators(t *testing.T) {
	// Nadie recuerda si tocaba coma o espacio, y ninguno aparece dentro de una
	// credencial, así que se aceptan todos.
	got := SplitKeys("  pa-uno, pa-dos;pa-tres\npa-cuatro ")
	want := []string{"pa-uno", "pa-dos", "pa-tres", "pa-cuatro"}
	if len(got) != len(want) {
		t.Fatalf("salieron %d credenciales: %v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("credencial %d = %q, esperaba %q", i, got[i], want[i])
		}
	}
	if len(SplitKeys("   ")) != 0 {
		t.Error("una variable vacía produjo credenciales")
	}
}

// quotaServer responde con cuota agotada a las credenciales de spent y
// correctamente a las demás, para probar la rotación contra un servidor real.
func quotaServer(t *testing.T, spent map[string]bool, seen *[]string) *Voyage {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		*seen = append(*seen, key)
		if spent[key] {
			w.WriteHeader(http.StatusPaymentRequired)
			fmt.Fprint(w, `{"detail":"You have exhausted your monthly quota"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"index": 0, "embedding": []int{1, 2, 3}}},
			"usage": map[string]int{"total_tokens": 10},
		})
	}))
	t.Cleanup(srv.Close)
	return &Voyage{
		Keys: NewKeyring("gastada", "buena"), EmbedModel: "voyage-code-3",
		Dimensions: 3, BaseURL: srv.URL, HTTP: srv.Client(),
	}
}

func TestVoyageKeepsWorkingWhenAKeyRunsOut(t *testing.T) {
	// El punto de la función: un indexado largo no debe caerse cuando se acaba
	// la cuota de la primera cuenta. La misma petición se reintenta con la
	// siguiente credencial y devuelve el vector.
	var seen []string
	v := quotaServer(t, map[string]bool{"gastada": true}, &seen)
	got, err := v.Embed(context.Background(), []string{"hola"}, KindDocument)
	if err != nil {
		t.Fatalf("la petición murió con la primera credencial: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("devolvió %d vectores", len(got))
	}
	if len(seen) != 2 || seen[0] != "gastada" || seen[1] != "buena" {
		t.Errorf("credenciales usadas = %v, esperaba probar la gastada y luego la buena", seen)
	}
	if v.Keys.Alive() != 1 {
		t.Errorf("quedan %d credenciales vivas, esperaba 1", v.Keys.Alive())
	}
}

func TestVoyageReportsQuotaOnlyWhenEveryKeyIsSpent(t *testing.T) {
	// Con todas agotadas sí hay que rendirse, y con un error que el usuario
	// pueda distinguir de un fallo transitorio.
	var seen []string
	v := quotaServer(t, map[string]bool{"gastada": true, "buena": true}, &seen)
	_, err := v.Embed(context.Background(), []string{"hola"}, KindDocument)
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("error = %v, esperaba ErrQuotaExhausted", err)
	}
	if len(seen) != 2 {
		t.Errorf("intentó %d veces, esperaba una por credencial: %v", len(seen), seen)
	}
}

func TestVoyageRotatesWhenRateLimitNeverClears(t *testing.T) {
	// El caso real de Voyage: no documenta ningún código para "cuota agotada" y
	// devuelve 429 tanto para una ráfaga pasajera como para una cuenta sin
	// margen. Una ráfaga cede a los pocos reintentos; una cuenta agotada no
	// cede nunca. Cuando el 429 sobrevive a todos los reintentos hay que probar
	// con otra credencial en vez de dar el trabajo por perdido.
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		seen = append(seen, key)
		if key == "sin-margen" {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"detail":"Rate limit exceeded"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"index": 0, "embedding": []int{1, 2, 3}}},
			"usage": map[string]int{"total_tokens": 10},
		})
	}))
	t.Cleanup(srv.Close)
	v := &Voyage{
		Keys: NewKeyring("sin-margen", "buena"), EmbedModel: "voyage-code-3",
		Dimensions: 3, BaseURL: srv.URL, HTTP: srv.Client(),
	}
	got, err := v.Embed(context.Background(), []string{"hola"}, KindDocument)
	if err != nil {
		t.Fatalf("no rotó ante un 429 persistente: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("devolvió %d vectores", len(got))
	}
	if seen[len(seen)-1] != "buena" {
		t.Errorf("la última petición usó %q, esperaba la credencial buena", seen[len(seen)-1])
	}
	if v.Keys.Alive() != 1 {
		t.Errorf("quedan %d credenciales vivas, esperaba 1", v.Keys.Alive())
	}
}

func TestVoyageDoesNotBurnKeysOnATransientRateLimit(t *testing.T) {
	// El reverso: una ráfaga que cede al segundo intento no debe costar una
	// credencial. Si se retirara a la primera, un pico de tráfico quemaría
	// todas las llaves buenas en cadena.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"detail":"Rate limit exceeded"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"index": 0, "embedding": []int{1, 2, 3}}},
			"usage": map[string]int{"total_tokens": 10},
		})
	}))
	t.Cleanup(srv.Close)
	v := &Voyage{
		Keys: NewKeyring("primera", "segunda"), EmbedModel: "voyage-code-3",
		Dimensions: 3, BaseURL: srv.URL, HTTP: srv.Client(),
	}
	if _, err := v.Embed(context.Background(), []string{"hola"}, KindDocument); err != nil {
		t.Fatal(err)
	}
	if v.Keys.Alive() != 2 {
		t.Errorf("se retiró una credencial por una ráfaga pasajera: quedan %d de 2", v.Keys.Alive())
	}
}
