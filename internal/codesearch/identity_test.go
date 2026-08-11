package codesearch

import (
	"os"
	"path/filepath"
	"testing"
)

// gitRepo arma un .git mínimo con el remoto indicado ("" para no ponerle uno).
func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n"
	if remote != "" {
		cfg += "[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestIdentifyWorkspaceUsesRemote(t *testing.T) {
	// Con remoto, la identidad sale de ahí: es lo único que sobrevive a clonar
	// el repositorio en otra máquina, que es el caso que hace útil un índice
	// remoto.
	w := IdentifyWorkspace(gitRepo(t, "https://github.com/esengine/DeepSeek-Reasonix.git"))
	if w.Source != SourceRemote {
		t.Errorf("Source = %q, esperaba %q", w.Source, SourceRemote)
	}
	if w.ID == "" {
		t.Error("ID vacío")
	}
}

func TestIdentifyWorkspaceSameIDAcrossCloneURLs(t *testing.T) {
	// Clonar por SSH o por HTTPS es el mismo repositorio. Si dieran IDs
	// distintos, cambiar de máquina reindexaría todo desde cero y volvería a
	// cobrar los embeddings.
	forms := []string{
		"https://github.com/esengine/DeepSeek-Reasonix.git",
		"https://github.com/esengine/DeepSeek-Reasonix",
		"git@github.com:esengine/DeepSeek-Reasonix.git",
		"ssh://git@github.com/esengine/DeepSeek-Reasonix.git",
		"https://token123@github.com/esengine/DeepSeek-Reasonix.git",
		"https://github.com/esengine/DeepSeek-Reasonix/",
	}
	want := IdentifyWorkspace(gitRepo(t, forms[0])).ID
	for _, f := range forms[1:] {
		if got := IdentifyWorkspace(gitRepo(t, f)).ID; got != want {
			t.Errorf("%q dio ID %s, esperaba %s", f, got, want)
		}
	}
}

func TestIdentifyWorkspaceIgnoresPathWhenRemoteExists(t *testing.T) {
	// Mover el proyecto de carpeta no debe cambiar su identidad: es el mismo
	// repositorio y su índice sigue siendo válido.
	a := gitRepo(t, "https://github.com/acme/app.git")
	b := gitRepo(t, "https://github.com/acme/app.git")
	if IdentifyWorkspace(a).ID != IdentifyWorkspace(b).ID {
		t.Error("dos rutas del mismo repositorio dieron identidades distintas")
	}
}

func TestIdentifyWorkspaceDifferentReposDiffer(t *testing.T) {
	// Dos proyectos distintos no pueden compartir índice, o la búsqueda
	// devolvería código de otro repositorio.
	a := IdentifyWorkspace(gitRepo(t, "https://github.com/acme/app.git"))
	b := IdentifyWorkspace(gitRepo(t, "https://github.com/acme/other.git"))
	if a.ID == b.ID {
		t.Error("dos repositorios distintos compartieron identidad")
	}
}

func TestIdentifyWorkspaceFallsBackToPath(t *testing.T) {
	// Sin git no hay identidad portable, pero tampoco hace falta: un proyecto
	// así vive en una sola máquina y la ruta lo identifica bien.
	root := t.TempDir()
	w := IdentifyWorkspace(root)
	if w.Source != SourcePath {
		t.Errorf("Source = %q, esperaba %q", w.Source, SourcePath)
	}
	if w.ID == "" {
		t.Error("ID vacío")
	}
}

func TestIdentifyWorkspaceRepoWithoutRemoteUsesPath(t *testing.T) {
	// Un repositorio local sin remoto tampoco tiene índice remoto que
	// reencontrar, así que la ruta basta.
	w := IdentifyWorkspace(gitRepo(t, ""))
	if w.Source != SourcePath {
		t.Errorf("Source = %q, esperaba %q", w.Source, SourcePath)
	}
}

func TestIdentifyWorkspaceFindsGitInAncestor(t *testing.T) {
	// Abrir reasonix en un subdirectorio del proyecto debe reusar el índice
	// del repositorio, no crear uno nuevo por carpeta.
	root := gitRepo(t, "https://github.com/acme/app.git")
	sub := filepath.Join(root, "internal", "agent")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if IdentifyWorkspace(sub).ID != IdentifyWorkspace(root).ID {
		t.Error("un subdirectorio del repositorio produjo otra identidad")
	}
}

func TestIdentifyWorkspaceResolvesWorktreeGitFile(t *testing.T) {
	// En un worktree, .git es un archivo que apunta al directorio real. Sin
	// resolverlo, cada worktree indexaría por separado el mismo repositorio.
	real := gitRepo(t, "https://github.com/acme/app.git")
	wt := t.TempDir()
	pointer := "gitdir: " + filepath.Join(real, ".git") + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	if IdentifyWorkspace(wt).Source != SourceRemote {
		t.Error("no se resolvió el .git de un worktree")
	}
	if IdentifyWorkspace(wt).ID != IdentifyWorkspace(real).ID {
		t.Error("el worktree no comparte identidad con su repositorio")
	}
}

func TestIdentifyWorkspaceNameIsBaseFolder(t *testing.T) {
	// El nombre es solo para mostrarlo en la interfaz.
	root := gitRepo(t, "https://github.com/acme/app.git")
	if got := IdentifyWorkspace(root).Name; got != filepath.Base(root) {
		t.Errorf("Name = %q, esperaba la carpeta base %q", got, filepath.Base(root))
	}
}
