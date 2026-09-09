package claudesessions

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDesktop(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatalf("escribir %s: %v", file, err)
	}
}

func TestLabelForDistinguishesLaunchersThatShareAHome(t *testing.T) {
	// Varios lanzadores usan el mismo CLAUDE_CONFIG_DIR y solo se diferencian
	// por la carpeta. Etiquetar por home los volvería a todos "asistente".
	t.Parallel()
	dir := t.TempDir()
	writeDesktop(t, dir, "asistente.desktop", "[Desktop Entry]\nName=Asistente Claude\n"+
		"Exec=gnome-terminal --working-directory=/home/u/asistente -- bash -c 'CLAUDE_CONFIG_DIR=/home/u/.claude-assistant claude'\n")
	writeDesktop(t, dir, "devactivo.desktop", "[Desktop Entry]\nName=devactivo Claude\n"+
		"Exec=gnome-terminal --working-directory=/home/u/devactivo -- bash -c 'CLAUDE_CONFIG_DIR=/home/u/.claude-assistant claude'\n")

	l := LoadLaunchers(dir, "/home/u/.claude")
	if got := LabelFor(l, "/home/u/.claude-assistant", "/home/u/asistente"); got != "Asistente" {
		t.Fatalf("etiqueta = %q, se esperaba Asistente", got)
	}
	if got := LabelFor(l, "/home/u/.claude-assistant", "/home/u/devactivo"); got != "devactivo" {
		t.Fatalf("etiqueta = %q, se esperaba devactivo", got)
	}
}

func TestLoadLaunchersAssumesTheMainHomeWhenNoneIsNamed(t *testing.T) {
	// Un lanzador sin CLAUDE_CONFIG_DIR usa el home principal, que es como
	// quedan los accesos hechos sobre la instalación normal.
	t.Parallel()
	dir := t.TempDir()
	writeDesktop(t, dir, "ghl.desktop", "[Desktop Entry]\nName=GHL Claude\n"+
		"Exec=gnome-terminal --working-directory=/home/u/GHL -- bash -c 'claude'\n")

	l := LoadLaunchers(dir, "/home/u/.claude")
	if len(l) != 1 || l[0].Home != "/home/u/.claude" {
		t.Fatalf("no heredó el home principal: %+v", l)
	}
	if got := LabelFor(l, "/home/u/.claude", "/home/u/GHL"); got != "GHL" {
		t.Fatalf("etiqueta = %q, se esperaba GHL", got)
	}
}

func TestLoadLaunchersIgnoresEntriesThatIdentifyNothing(t *testing.T) {
	// Un manejador de URLs o un lanzador sin carpeta ni home no dice de qué
	// entorno viene una sesión, así que no debe competir por etiquetarla.
	t.Parallel()
	dir := t.TempDir()
	writeDesktop(t, dir, "url.desktop", "[Desktop Entry]\nName=Claude Code URL Handler\nExec=claude --url %u\n")
	writeDesktop(t, dir, "otro.desktop", "[Desktop Entry]\nName=Editor\nExec=gedit %f\n")
	if l := LoadLaunchers(dir, "/home/u/.claude"); len(l) != 0 {
		t.Fatalf("se colaron lanzadores sin identidad: %+v", l)
	}
}

func TestCleanLauncherNameKeepsWhatDistinguishes(t *testing.T) {
	// "Claude", "Code", "Resume" y "(proxy)" aparecen en casi todos, así que no
	// distinguen nada y estorban en un chip.
	t.Parallel()
	cases := map[string]string{
		"Asistente Claude":         "Asistente",
		"devactivo Claude (proxy)": "devactivo",
		"Deepseek Resume":          "Deepseek",
		"GHL Claude":               "GHL",
		"Claude":                   "Claude",
	}
	for raw, want := range cases {
		if got := cleanLauncherName(raw); got != want {
			t.Fatalf("cleanLauncherName(%q) = %q, se esperaba %q", raw, got, want)
		}
	}
}
