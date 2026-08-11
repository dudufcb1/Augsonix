package cli

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/codesearch"
)

func TestProgressBarShowsCountAndPercent(t *testing.T) {
	// La espera a ciegas es lo que hace insoportable un indexado largo: el
	// conteo dice cuánto falta y el porcentaje si conviene esperar.
	var out strings.Builder
	b := &progressBar{out: &out, start: time.Now()}
	b.update(codesearch.Progress{Done: 50, Total: 200, Embedded: 12})
	got := out.String()
	if !strings.Contains(got, "50/200") || !strings.Contains(got, "25%") {
		t.Errorf("la barra no muestra avance legible: %q", got)
	}
	if !strings.Contains(got, "12 embebidos") {
		t.Errorf("no dice cuántos se embebieron: %q", got)
	}
}

func TestProgressBarSurvivesUnknownTotal(t *testing.T) {
	// Entre que arranca el escaneo y se sabe cuántos archivos hay, Total es
	// cero; dividir ahí tumbaría el comando entero.
	var out strings.Builder
	b := &progressBar{out: &out, start: time.Now()}
	b.update(codesearch.Progress{Done: 3, Total: 0})
	if b.width != 0 {
		t.Error("pintó una barra sin saber el total")
	}
}

func TestHumanCountReadsAtAGlance(t *testing.T) {
	// El consumo se reporta para que el usuario sepa qué se llevó de su cuota;
	// "6.1M" se entiende y "6123456" no.
	cases := map[int64]string{500: "500", 1500: "1.5K", 6_100_000: "6.1M"}
	for in, want := range cases {
		if got := humanCount(in); got != want {
			t.Errorf("humanCount(%d) = %q, esperaba %q", in, got, want)
		}
	}
}

func TestHasFlagFindsForce(t *testing.T) {
	// --force reconstruye desde cero; confundirlo con su ausencia borraría un
	// índice que el usuario quería conservar, o al revés.
	if !hasFlag([]string{"--force"}, "--force") {
		t.Error("no reconoció --force")
	}
	if hasFlag([]string{"--dry-run"}, "--force") {
		t.Error("reconoció --force donde no estaba")
	}
}
