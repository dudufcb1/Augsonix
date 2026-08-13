package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/codesearch"
)

func TestGateOffNeverBlocks(t *testing.T) {
	// Apagada es el comportamiento de siempre: nadie que no pidió fricción debe
	// encontrarse con grep bloqueado.
	g := &SearchGate{Mode: FrictionOff}
	for range 10 {
		if err := g.Check(false); err != nil {
			t.Fatalf("bloqueó con la fricción apagada: %v", err)
		}
	}
}

func TestGateStrictBlocksAfterLimit(t *testing.T) {
	// Las primeras pasan —saber el string exacto es un uso legítimo de grep— y
	// la que se pasa del límite es la que delata exploración a ciegas.
	g := &SearchGate{Mode: FrictionStrict, Limit: 3}
	for i := range 3 {
		if err := g.Check(false); err != nil {
			t.Fatalf("bloqueó en la búsqueda %d, antes del límite: %v", i+1, err)
		}
	}
	err := g.Check(false)
	if err == nil {
		t.Fatal("no bloqueó al pasarse del límite")
	}
	if !strings.Contains(err.Error(), "code_search") {
		t.Errorf("el error no dice qué hacer: %v", err)
	}
}

func TestGateStrictIgnoresBypass(t *testing.T) {
	// En estricto no hay pase: si lo hubiera, el modo sería el semi.
	g := &SearchGate{Mode: FrictionStrict, Limit: 1}
	_ = g.Check(false)
	if err := g.Check(true); err == nil {
		t.Error("el modo estricto aceptó el pase")
	}
}

func TestGateSemiLetsDeclaredSearchThrough(t *testing.T) {
	// El punto del modo semi: no le quita la herramienta, le pide reconocer que
	// decidió no buscar por significado.
	g := &SearchGate{Mode: FrictionSemi, Limit: 2}
	_ = g.Check(false)
	_ = g.Check(false)
	if err := g.Check(false); err == nil {
		t.Fatal("no pidió la declaración al pasarse del límite")
	}
	if err := g.Check(true); err != nil {
		t.Errorf("rechazó una búsqueda declarada: %v", err)
	}
}

func TestGateSemiAsksAgainAfterBypass(t *testing.T) {
	// El pase vale por una. Si valiera para siempre, el modelo lo pondría una
	// vez y la fricción dejaría de existir.
	g := &SearchGate{Mode: FrictionSemi, Limit: 2}
	_ = g.Check(false)
	_ = g.Check(false)
	if err := g.Check(true); err != nil {
		t.Fatal(err)
	}
	_ = g.Check(false)
	_ = g.Check(false)
	if err := g.Check(false); err == nil {
		t.Error("tras usar el pase ya no volvió a pedir nada")
	}
}

func TestGateSemanticSearchResetsStreak(t *testing.T) {
	// Usar el índice libera grep. Así el mecanismo es un incentivo y no un
	// castigo: haz lo que se te pide y recuperas la herramienta.
	g := &SearchGate{Mode: FrictionStrict, Limit: 2}
	_ = g.Check(false)
	_ = g.Check(false)
	g.RecordSemantic()
	if g.Streak() != 0 {
		t.Errorf("Streak = %d tras una búsqueda semántica, esperaba 0", g.Streak())
	}
	if err := g.Check(false); err != nil {
		t.Errorf("siguió bloqueando después de usar el índice: %v", err)
	}
}

func TestGateStandsDownWhenIndexUnusable(t *testing.T) {
	// Sin índice utilizable —aún construyéndose, sin configurar, sin cuota—
	// bloquear grep dejaría al modelo sin ninguna forma de buscar.
	g := &SearchGate{Mode: FrictionStrict, Limit: 1, Usable: func() bool { return false }}
	for range 5 {
		if err := g.Check(false); err != nil {
			t.Fatalf("bloqueó sin un índice al que mandar al modelo: %v", err)
		}
	}
}

func TestGateNilIsHarmless(t *testing.T) {
	// La mayoría de los flujos no configuran fricción y el puntero llega nil.
	var g *SearchGate
	if err := g.Check(false); err != nil {
		t.Errorf("un gate nil bloqueó: %v", err)
	}
	g.RecordSemantic()
}

func TestGateIsSafeUnderConcurrentTools(t *testing.T) {
	// El agente paraleliza las herramientas de solo lectura, así que grep y
	// code_search pueden entrar a la vez desde goroutines distintas.
	g := &SearchGate{Mode: FrictionSemi, Limit: 3}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { ; _ = g.Check(false) })
		wg.Go(func() { ; g.RecordSemantic() })
	}
	wg.Wait()
}

func TestGrepSchemaOnlyOffersBypassInSemiMode(t *testing.T) {
	// El esquema viaja en el prefijo estable del prompt. El campo solo aparece
	// donde sirve, y como el modo se fija al arrancar, dentro de una sesión el
	// esquema nunca cambia: mutarlo tiraría la caché del proveedor cada vez.
	cases := []struct {
		mode SearchFriction
		want bool
	}{
		{FrictionOff, false},
		{FrictionSemi, true},
		{FrictionStrict, false},
	}
	for _, c := range cases {
		g := grepTool{gate: &SearchGate{Mode: c.mode}}
		has := strings.Contains(string(g.Schema()), "no_semantic_needed")
		if has != c.want {
			t.Errorf("modo %q: no_semantic_needed presente=%v, esperaba %v", c.mode, has, c.want)
		}
	}
}

func TestGrepSchemaUnchangedWithoutGate(t *testing.T) {
	// Sin fricción configurada, grep tiene que quedar byte-idéntico al de
	// siempre: quien no pidió esto no debe pagar un solo token extra.
	plain := string(grepTool{}.Schema())
	off := string(grepTool{gate: &SearchGate{Mode: FrictionOff}}.Schema())
	if plain != off {
		t.Error("el esquema de grep cambió con la fricción apagada")
	}
	if strings.Contains(plain, "no_semantic_needed") {
		t.Error("el esquema base ya trae el campo de la fricción")
	}
}

func TestGrepSchemaIsStableAcrossCalls(t *testing.T) {
	// Dos llamadas seguidas deben dar exactamente lo mismo aunque el contador
	// haya avanzado entre medias: el contador cambia el comportamiento, no el
	// contrato que ve el modelo.
	g := grepTool{gate: &SearchGate{Mode: FrictionSemi, Limit: 1}}
	before := string(g.Schema())
	_ = g.gate.Check(false)
	_ = g.gate.Check(false)
	if after := string(g.Schema()); after != before {
		t.Error("el esquema de grep cambió al avanzar el contador de fricción")
	}
}

func TestSemanticSearchUnblocksGrep(t *testing.T) {
	// El circuito completo: grep se traba, el modelo usa el índice y grep
	// vuelve a funcionar. Es lo que hace del mecanismo un incentivo.
	gate := &SearchGate{Mode: FrictionStrict, Limit: 2}
	for range 2 {
		if err := gate.Check(false); err != nil {
			t.Fatal(err)
		}
	}
	if err := gate.Check(false); err == nil {
		t.Fatal("grep no se trabó al pasarse del límite")
	}

	cs := NewCodeSearch(&fakeIndex{chunks: 10, results: []codesearch.Result{
		result("internal/auth.go", 1, 5, 0.9, "func Authenticate() {}"),
	}}, gate)
	if _, err := cs.Execute(context.Background(), json.RawMessage(`{"request":"validacion de sesion"}`)); err != nil {
		t.Fatal(err)
	}
	if err := gate.Check(false); err != nil {
		t.Errorf("grep siguió trabado después de usar el índice: %v", err)
	}
}

func TestFailedSemanticSearchDoesNotUnblockGrep(t *testing.T) {
	// Una búsqueda que falló no cumplió el trato: si liberara grep igual, el
	// modelo podría desbloquearlo llamando al índice de cualquier manera.
	gate := &SearchGate{Mode: FrictionStrict, Limit: 1}
	_ = gate.Check(false)
	cs := NewCodeSearch(&fakeIndex{chunks: 10, err: errors.New("503")}, gate)
	_, _ = cs.Execute(context.Background(), json.RawMessage(`{"request":"algo"}`))
	if err := gate.Check(false); err == nil {
		t.Error("una búsqueda fallida liberó grep")
	}
}
