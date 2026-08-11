package cli

import "testing"

func TestThemedCommandsCoverTheirAliases(t *testing.T) {
	// hook y hooks son el mismo comando. Al pasar del switch a la tabla, un
	// alias olvidado dejaría de existir sin que nada fallara al compilar.
	if themedCommands["hook"] == nil || themedCommands["hooks"] == nil {
		t.Error("falta uno de los alias de hook")
	}
}

func TestThemedCommandsAreAllReachable(t *testing.T) {
	// La tabla es ahora la única puerta de estos subcomandos: una entrada nil
	// los volvería inalcanzables y solo se notaría al invocarlos.
	for name, run := range themedCommands {
		if run == nil {
			t.Errorf("el subcomando %q quedó sin función", name)
		}
	}
	for _, want := range []string{"codesearch", "config", "task", "session", "review"} {
		if themedCommands[want] == nil {
			t.Errorf("el subcomando %q no está registrado", want)
		}
	}
}
