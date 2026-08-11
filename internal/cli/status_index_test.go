package cli

import (
	"strings"
	"testing"

	"reasonix/internal/control"
)

func TestIndexStatusBodyShowsChunkCountWhenReady(t *testing.T) {
	// Con el índice al día se muestra cuántos chunks tiene. Sin ese número,
	// "indexado" y "no configurado" se ven igual —vacíos— y no hay forma de
	// saber si la búsqueda semántica está funcionando.
	got := indexStatusBody(control.IndexStatus{Phase: "ready", Chunks: 4000})
	if !strings.Contains(got, "4000") {
		t.Errorf("no mostró el conteo de chunks: %q", got)
	}
}

func TestIndexStatusBodyHiddenWhenNothingIndexed(t *testing.T) {
	// En reposo o con el índice vacío no hay nada que reportar.
	for _, st := range []control.IndexStatus{{Phase: "idle", Chunks: 4000}, {Phase: "ready", Chunks: 0}} {
		if got := indexStatusBody(st); got != "" {
			t.Errorf("%+v mostró %q, esperaba nada", st, got)
		}
	}
}

func TestIndexStatusBodyAnnouncesFirstScan(t *testing.T) {
	// El primer indexado de un proyecto tarda, así que se avisa desde que
	// empieza a recorrer, antes de tener siquiera un total que mostrar.
	got := indexStatusBody(control.IndexStatus{Phase: "scanning", First: true})
	if got == "" {
		t.Error("el primer escaneo no avisó nada")
	}
}

func TestIndexStatusBodyQuietOnLaterScans(t *testing.T) {
	// Abrir una sesión con el índice al día recorre el árbol en milisegundos.
	// Mostrar eso haría parpadear el pie de pantalla en cada arranque.
	if got := indexStatusBody(control.IndexStatus{Phase: "scanning", First: false}); got != "" {
		t.Errorf("un escaneo posterior mostró %q, esperaba silencio", got)
	}
}

func TestIndexProgressShowsCountAndPercent(t *testing.T) {
	// El conteo dice cuánto falta y el porcentaje si conviene esperar o seguir
	// trabajando; por eso van los dos.
	got := indexProgressText(control.IndexStatus{Done: 123, Total: 450})
	if !strings.Contains(got, "123/450") {
		t.Errorf("falta el conteo en %q", got)
	}
	if !strings.Contains(got, "27%") {
		t.Errorf("falta el porcentaje en %q", got)
	}
}

func TestIndexProgressSurvivesUnknownTotal(t *testing.T) {
	// Entre que arranca el escaneo y se sabe cuántos archivos hay, Total es
	// cero. Dividir ahí entre cero tumbaría la interfaz entera.
	got := indexProgressText(control.IndexStatus{Done: 7, Total: 0})
	if got == "" || strings.Contains(got, "%") {
		t.Errorf("con total desconocido dio %q; esperaba solo el conteo", got)
	}
}

func TestIndexStatusBodyShowsFailure(t *testing.T) {
	// Si el proveedor falla, el usuario tiene que enterarse: si no, vería un
	// indicador congelado sin saber que ya no va a avanzar.
	if got := indexStatusBody(control.IndexStatus{Phase: "failed"}); got == "" {
		t.Error("un fallo del índice no se reportó en el pie de pantalla")
	}
}

func TestIndexStatusBodyDistinguishesQuotaFromFailure(t *testing.T) {
	// La cuota agotada y un fallo cualquiera piden acciones distintas: una se
	// arregla reponiendo la cuenta y el otro esperando o revisando la red. Si
	// se vieran igual, el usuario no sabría cuál de las dos le tocó.
	quota := indexStatusBody(control.IndexStatus{Phase: "quota"})
	failed := indexStatusBody(control.IndexStatus{Phase: "failed"})
	if quota == "" {
		t.Fatal("la cuota agotada no se reportó")
	}
	if quota == failed {
		t.Error("la cuota agotada se ve igual que un fallo cualquiera")
	}
}
