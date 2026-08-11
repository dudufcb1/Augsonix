package builtin

import (
	"fmt"
	"sync"
)

// SearchFriction decide qué pasa cuando el modelo encadena búsquedas léxicas
// sin consultar el índice semántico.
type SearchFriction string

const (
	// FrictionOff deja grep como siempre.
	FrictionOff SearchFriction = "off"
	// FrictionSemi exige declarar no_semantic_needed para seguir con grep. No
	// le quita la herramienta: le pide que reconozca que decidió no buscar por
	// significado, que es distinto de habérsele olvidado.
	FrictionSemi SearchFriction = "semi"
	// FrictionStrict corta y manda a code_search.
	FrictionStrict SearchFriction = "strict"
)

// defaultFrictionLimit son las búsquedas léxicas seguidas que se toleran antes
// de intervenir. Tres es donde el patrón deja de ser "sé lo que busco" y empieza
// a ser exploración a ciegas, que es justo lo que el índice hace mejor.
const defaultFrictionLimit = 3

// SearchGate cuenta búsquedas léxicas seguidas y aplica la fricción al pasarse
// del límite. Lo comparten grep y code_search, que pueden llamarse desde
// goroutines distintas. El contador se reinicia con cada búsqueda semántica,
// así que esto es un incentivo —usa el índice y grep queda libre— y no un
// castigo.
type SearchGate struct {
	mu     sync.Mutex
	streak int

	// Mode y Limit se fijan al arrancar y no cambian durante la sesión: el
	// esquema de grep depende del modo, y mutarlo a media sesión tiraría la
	// caché de prefijo del proveedor.
	Mode  SearchFriction
	Limit int
	// Usable reporta si el índice puede responder ahora mismo. Sin él la
	// fricción se desactiva sola: bloquear grep cuando no hay a dónde mandar al
	// modelo lo deja sin ninguna forma de buscar.
	Usable func() bool
}

// Check registra una búsqueda léxica y devuelve el motivo por el que no debe
// proceder, o nil si puede. bypass es la declaración explícita del modelo de
// que esta búsqueda no necesita significado.
func (g *SearchGate) Check(bypass bool) error {
	if g == nil || g.Mode == "" || g.Mode == FrictionOff {
		return nil
	}
	if g.Usable != nil && !g.Usable() {
		return nil
	}
	limit := g.Limit
	if limit <= 0 {
		limit = defaultFrictionLimit
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.streak < limit {
		g.streak++
		return nil
	}
	if g.Mode == FrictionSemi && bypass {
		// El pase vale por una: el contador vuelve a cero y a las siguientes
		// búsquedas se le pedirá otra vez.
		g.streak = 0
		return nil
	}
	return frictionError(g.Mode, limit)
}

// RecordSemantic reinicia el contador tras una búsqueda semántica.
func (g *SearchGate) RecordSemantic() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.streak = 0
}

// Streak es cuántas búsquedas léxicas seguidas van, para los tests.
func (g *SearchGate) Streak() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.streak
}

// frictionError le dice al modelo qué hacer, no solo que se equivocó: un error
// sin salida le hace gastar el turno adivinando.
func frictionError(mode SearchFriction, limit int) error {
	if mode == FrictionSemi {
		return fmt.Errorf("%d consecutive text searches without a semantic lookup. "+
			"Call code_search describing what you need, or repeat this grep with "+
			"no_semantic_needed=true if you already know the exact string", limit)
	}
	return fmt.Errorf("%d consecutive text searches without a semantic lookup. "+
		"Call code_search describing what you are looking for; grep is available "+
		"again right after", limit)
}
