package codesearch

import (
	"errors"
	"net/http"
	"strings"
)

// ErrQuotaExhausted marca que ninguna credencial acepta ya trabajo. Se
// distingue del resto de errores porque no se arregla reintentando: hay que
// reponer la cuenta o dar de alta otra llave, y el usuario necesita enterarse.
var ErrQuotaExhausted = errors.New("codesearch: se agotó la cuota del proveedor de embeddings")

// keyUnusable decide si conviene dejar esta credencial y probar con otra. Un
// 401 la descarta por sí solo: una llave revocada o mal copiada no mejora
// reintentando. El aviso de agregar método de pago NO cuenta: acompaña al
// límite de tasa reducido, que sí se resuelve esperando, y confundirlos
// quemaría las credenciales buenas en una ráfaga pasajera.
func keyUnusable(status int, detail string) bool {
	if status == http.StatusPaymentRequired || status == http.StatusUnauthorized {
		return true
	}
	d := strings.ToLower(detail)
	for _, sign := range []string{
		"insufficient",
		"exhausted",
		"exceeded your",
		"out of credit",
		"no remaining",
		"quota exceeded",
	} {
		if strings.Contains(d, sign) {
			return true
		}
	}
	return false
}
