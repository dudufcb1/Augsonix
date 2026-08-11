package codesearch

import (
	"errors"
	"net/http"
	"strings"
)

// ErrQuotaExhausted marca que el proveedor ya no acepta trabajo por cuota o
// saldo. Se distingue del resto de errores porque no se arregla reintentando:
// hay que reponer la cuenta, y el usuario necesita enterarse para hacerlo.
var ErrQuotaExhausted = errors.New("codesearch: se agotó la cuota del proveedor de embeddings")

// quotaExhausted decide si una respuesta significa "se acabó". El aviso de
// agregar método de pago también acompaña al límite de tasa reducido, que sí es
// reintentable, así que ese texto por sí solo no cuenta: solo el código 402 y
// las frases que dicen explícitamente que la cuota se terminó.
func quotaExhausted(status int, detail string) bool {
	if status == http.StatusPaymentRequired {
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
