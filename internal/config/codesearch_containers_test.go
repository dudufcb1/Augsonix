package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsContainerMatchesOnlyTheFolderItself(t *testing.T) {
	// La lista existe para que las SUBcarpetas sí se indexen. Si la
	// coincidencia fuera por prefijo, marcar un contenedor apagaría el índice
	// de todos los proyectos que viven dentro, que es lo contrario de la idea.
	base := t.TempDir()
	cfg := CodeSearchConfig{Containers: []string{base}}
	if !cfg.IsContainer(base) {
		t.Error("no reconoció la carpeta listada")
	}
	if cfg.IsContainer(filepath.Join(base, "proyecto")) {
		t.Error("marcó como contenedor a una subcarpeta")
	}
	if cfg.IsContainer(filepath.Dir(base)) {
		t.Error("marcó como contenedor a la carpeta de arriba")
	}
}

func TestIsContainerNormalizesHowThePathIsWritten(t *testing.T) {
	// Nadie escribe la ruta dos veces igual: con barra final, relativa o con
	// "~". Todas apuntan al mismo sitio y todas deben coincidir.
	base := t.TempDir()
	for _, escrito := range []string{base, base + "/", base + "/./", filepath.Join(base, "x", "..")} {
		cfg := CodeSearchConfig{Containers: []string{escrito}}
		if !cfg.IsContainer(base) {
			t.Errorf("escrito como %q no coincidió", escrito)
		}
	}
}

func TestIsContainerExpandsHome(t *testing.T) {
	// La forma natural de escribirlo en la configuración es "~/asistente".
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("sin directorio de usuario")
	}
	cfg := CodeSearchConfig{Containers: []string{"~/asistente"}}
	if !cfg.IsContainer(filepath.Join(home, "asistente")) {
		t.Error("no expandió ~ al comparar")
	}
	if cfg.IsContainer(filepath.Join(home, "otra-cosa")) {
		t.Error("coincidió con una carpeta distinta")
	}
}

func TestIsContainerIgnoresEmptyEntries(t *testing.T) {
	// Una línea en blanco en la lista no puede convertir todo en contenedor.
	cfg := CodeSearchConfig{Containers: []string{"", "   "}}
	if cfg.IsContainer(t.TempDir()) {
		t.Error("una entrada vacía marcó como contenedor")
	}
	if cfg.IsContainer("") {
		t.Error("una ruta vacía se dio por contenedor")
	}
}

func TestIsContainerWithoutListNeverMatches(t *testing.T) {
	// Sin lista, el comportamiento es el de siempre: todo se puede indexar.
	var cfg CodeSearchConfig
	if cfg.IsContainer(t.TempDir()) {
		t.Error("sin lista configurada dijo que era contenedor")
	}
}
