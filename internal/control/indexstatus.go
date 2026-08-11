package control

import "sync"

// IndexStatus resume el avance del índice semántico para los frontends. Es un
// tipo propio y no el del motor para que el controlador no dependa del backend
// de vectores: la TUI, el servidor HTTP y el escritorio pintan lo mismo sin
// saber quién indexa.
type IndexStatus struct {
	// Phase es "idle", "scanning", "indexing", "ready", "failed" o "quota".
	Phase string
	// Done y Total cuentan archivos, la unidad que el usuario reconoce.
	Done, Total int
	Chunks      int
	// First marca el primer indexado del proyecto, que es el que tarda.
	First bool
	Err   error
}

// indexStatusSource permite que el estado se consulte en cada fotograma sin que
// el controlador tenga que suscribirse a nada.
type indexStatusSource struct {
	mu sync.RWMutex
	fn func() IndexStatus
}

// SetIndexStatusFunc registra de dónde sale el estado del índice. Lo cablea el
// ensamblaje; sin él los frontends simplemente no muestran indicador.
func (c *Controller) SetIndexStatusFunc(fn func() IndexStatus) {
	c.indexStatus.mu.Lock()
	defer c.indexStatus.mu.Unlock()
	c.indexStatus.fn = fn
}

// IndexStatus devuelve el avance del índice, y ok=false cuando no hay índice
// configurado, que es el caso normal.
func (c *Controller) IndexStatus() (IndexStatus, bool) {
	c.indexStatus.mu.RLock()
	fn := c.indexStatus.fn
	c.indexStatus.mu.RUnlock()
	if fn == nil {
		return IndexStatus{}, false
	}
	return fn(), true
}
