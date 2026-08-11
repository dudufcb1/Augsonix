package codesearch

import "sync"

// Phase es en qué anda el índice. La interfaz lo pinta sin saber nada del motor.
type Phase string

const (
	// PhaseIdle es el estado en reposo: no hay escaneo en curso.
	PhaseIdle Phase = "idle"
	// PhaseScanning recorre el workspace decidiendo qué cambió. Es rápido y no
	// gasta cuota, así que casi no se alcanza a ver.
	PhaseScanning Phase = "scanning"
	// PhaseIndexing embebe lo que cambió. Es la fase que tarda y la que cuesta.
	PhaseIndexing Phase = "indexing"
	// PhaseReady tiene el índice al día.
	PhaseReady Phase = "ready"
	// PhaseFailed guarda el error para poder explicarlo en vez de quedarse mudo.
	PhaseFailed Phase = "failed"
	// PhaseQuota es el caso que hay que poder distinguir de un vistazo: no es
	// un fallo pasajero, es que se acabó la cuota y toca reponer la cuenta.
	PhaseQuota Phase = "quota"
)

// Status es una foto del avance, copiable y segura de leer desde otra goroutine.
type Status struct {
	Phase Phase
	// Done y Total cuentan archivos, no chunks: es lo que el usuario reconoce.
	Done, Total int
	// Chunks es lo que hay indexado ahora mismo, aunque el escaneo siga.
	Chunks int
	// Embedded son los archivos que sí hubo que reembeber en este escaneo; con
	// el índice al día se queda en cero y el escaneo pasa desapercibido.
	Embedded int
	// First distingue el primer indexado de un repositorio de los siguientes,
	// que es la diferencia entre esperar minutos y no notar nada.
	First bool
	Err   error
}

// progress lleva el avance del escaneo. Vive aparte del Index porque lo escribe
// la goroutine que indexa y lo lee la interfaz en cada fotograma.
type progress struct {
	mu sync.RWMutex
	st Status
}

// snapshot normaliza el valor cero a PhaseIdle, para que un índice recién
// construido reporte reposo en vez de una fase vacía.
func (p *progress) snapshot() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st := p.st
	if st.Phase == "" {
		st.Phase = PhaseIdle
	}
	return st
}

func (p *progress) set(mutate func(*Status)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	mutate(&p.st)
}

// Status devuelve el avance actual del índice.
func (ix *Index) Status() Status {
	return ix.progress.snapshot()
}
