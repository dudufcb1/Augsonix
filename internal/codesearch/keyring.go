package codesearch

import (
	"strings"
	"sync"
)

// Keyring reparte las credenciales del proveedor y retira la que se quedó sin
// cuota. Existe porque un indexado largo no debe caerse a la mitad: cuando una
// llave se agota entra la siguiente y el trabajo sigue donde iba.
type Keyring struct {
	mu      sync.Mutex
	keys    []string
	retired []bool
	active  int
	// onRetire avisa que una llave se agotó. El usuario tiene que enterarse
	// para reponerla, y si nadie se lo dice se queda sin margen sin saberlo.
	onRetire func(spent, alive, total int)
}

// NewKeyring arma el anillo con las llaves dadas, en orden de uso. Las vacías y
// las repetidas se descartan: una llave repetida daría por buena una cuota que
// ya se agotó.
func NewKeyring(keys ...string) *Keyring {
	k := &Keyring{}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		k.keys = append(k.keys, key)
	}
	k.retired = make([]bool, len(k.keys))
	return k
}

// OnRetire registra a quién avisar cuando una llave se agote.
func (k *Keyring) OnRetire(fn func(spent, alive, total int)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.onRetire = fn
}

// Current devuelve la llave en uso y su posición. El segundo valor es falso
// cuando ya no queda ninguna con cuota.
func (k *Keyring) Current() (string, int, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.active >= len(k.keys) {
		return "", 0, false
	}
	return k.keys[k.active], k.active, true
}

// Retire marca como agotada la llave de la posición dada y avanza a la
// siguiente con cuota. Devuelve si quedó alguna utilizable. Recibe la posición
// y no la llave para que dos peticiones en paralelo que fallen con la misma
// credencial no retiren dos llaves distintas.
func (k *Keyring) Retire(index int) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if index < 0 || index >= len(k.keys) || k.retired[index] {
		return k.active < len(k.keys)
	}
	k.retired[index] = true
	for k.active < len(k.keys) && k.retired[k.active] {
		k.active++
	}
	alive := k.aliveLocked()
	if k.onRetire != nil {
		k.onRetire(index+1, alive, len(k.keys))
	}
	return alive > 0
}

// Len es cuántas llaves se configuraron.
func (k *Keyring) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.keys)
}

// Alive es cuántas conservan cuota.
func (k *Keyring) Alive() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.aliveLocked()
}

func (k *Keyring) aliveLocked() int {
	n := 0
	for i := range k.keys {
		if !k.retired[i] {
			n++
		}
	}
	return n
}

// SplitKeys separa varias credenciales escritas en una sola variable de
// entorno. Se aceptan comas, punto y coma o espacios porque nadie recuerda cuál
// tocaba, y ninguno aparece dentro de una credencial.
func SplitKeys(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
