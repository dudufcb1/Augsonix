package codesearch

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepoWithRemote arma un repositorio con remoto y las subcarpetas pedidas.
func gitRepoWithRemote(t *testing.T, subdirs ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está disponible")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "https://github.com/alguien/proyecto.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	for _, d := range subdirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIdentifySeparatesSubprojectsInsideAContainer(t *testing.T) {
	// Dentro de una carpeta contenedora cada subcarpeta es un proyecto. Con la
	// misma identidad compartirían índice, y al sincronizar la segunda borraría
	// los archivos de la primera por no verlos desde su raíz.
	root := gitRepoWithRemote(t, "uno", "dos")
	a := IdentifyWorkspaceIn(filepath.Join(root, "uno"), []string{root})
	b := IdentifyWorkspaceIn(filepath.Join(root, "dos"), []string{root})
	if a.ID == b.ID {
		t.Errorf("dos subproyectos del contenedor comparten identidad: %s", a.ID)
	}
	if a.Source != SourceRemote {
		t.Errorf("la identidad no salió del remoto: %s", a.Source)
	}
}

func TestIdentifyKeepsSubdirsTogetherOutsideAContainer(t *testing.T) {
	// El caso que no hay que romper: quien abre internal/agent de su propio
	// repositorio quiere el índice del proyecto entero, no uno por carpeta.
	root := gitRepoWithRemote(t, "internal/agent")
	sub := filepath.Join(root, "internal", "agent")
	if IdentifyWorkspaceIn(sub, nil).ID != IdentifyWorkspaceIn(root, nil).ID {
		t.Error("un subdirectorio normal dejó de compartir índice con su repositorio")
	}
	if IdentifyWorkspaceIn(sub, []string{"/otra/cosa"}).ID != IdentifyWorkspaceIn(root, nil).ID {
		t.Error("una lista que no incluye este repositorio cambió la identidad")
	}
}

func TestIdentifyKeepsTheContainerRootUnchanged(t *testing.T) {
	// La raíz conserva la identidad de siempre aunque esté listada: cambiarla
	// dejaría huérfano cualquier índice ya construido.
	root := gitRepoWithRemote(t)
	if IdentifyWorkspaceIn(root, []string{root}).ID != IdentifyWorkspaceIn(root, nil).ID {
		t.Error("listar la carpeta cambió la identidad de su propia raíz")
	}
}

func TestIdentifySubprojectSurvivesMovingTheClone(t *testing.T) {
	// La gracia de identificar por remoto es reencontrar el índice en otra
	// máquina; eso debe seguir valiendo para un subproyecto de un contenedor.
	uno := gitRepoWithRemote(t, "app")
	dos := gitRepoWithRemote(t, "app")
	if IdentifyWorkspaceIn(filepath.Join(uno, "app"), []string{uno}).ID !=
		IdentifyWorkspaceIn(filepath.Join(dos, "app"), []string{dos}).ID {
		t.Error("el mismo subproyecto en otro clon dio otra identidad")
	}
}

func TestIdentifySubprojectIsStableAcrossCalls(t *testing.T) {
	// Abrir la misma subcarpeta dos veces da la misma identidad; si no, cada
	// sesión reindexaría desde cero.
	root := gitRepoWithRemote(t, "uno")
	sub := filepath.Join(root, "uno")
	if IdentifyWorkspaceIn(sub, []string{root}).ID != IdentifyWorkspaceIn(sub+"/", []string{root}).ID {
		t.Error("la barra final cambió la identidad")
	}
}
