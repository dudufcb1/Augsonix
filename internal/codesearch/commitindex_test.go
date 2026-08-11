package codesearch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newCommitIndex arma un índice de commits sobre un almacén local temporal y
// devuelve el almacén para poder reabrirlo con otros parámetros.
func newCommitIndex(t *testing.T, root string, limit int) (*CommitIndex, *fakeEmbedder, VectorStore) {
	t.Helper()
	emb := &fakeEmbedder{dims: 8}
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "commits"), emb.Model(), emb.dims)
	if err != nil {
		t.Fatal(err)
	}
	return NewCommitIndex(root, store, emb, nil, limit), emb, store
}

// addCommit agrega un commit al repositorio de prueba.
func addCommit(t *testing.T, root, subject, body string) {
	t.Helper()
	name := filepath.Join(root, "otro.txt")
	if err := os.WriteFile(name, []byte(subject+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", subject, "-m", body}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
}

func TestCommitSyncIndexesTheHistory(t *testing.T) {
	// El primer escaneo embebe todos los commits que quepan en el límite.
	root := newGitRepo(t, [2]string{"uno", "cuerpo uno"}, [2]string{"dos", "cuerpo dos"})
	ix, emb, _ := newCommitIndex(t, root, 10)
	stats, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Scanned != 2 || stats.Embedded != 2 {
		t.Fatalf("escaneados %d, embebidos %d; esperaba 2 y 2", stats.Scanned, stats.Embedded)
	}
	if emb.callCount() == 0 {
		t.Error("no se llamó al embebedor")
	}
	if n, ok := ix.Ready(); !ok || n != 2 {
		t.Errorf("Ready = (%d, %v), esperaba (2, true)", n, ok)
	}
}

func TestCommitSyncSkipsWhatAlreadyIndexed(t *testing.T) {
	// Un commit es inmutable: una vez indexado no se vuelve a embeber nunca.
	// Sin esto, cada arranque volvería a pagar la historia entera.
	root := newGitRepo(t, [2]string{"uno", ""}, [2]string{"dos", ""})
	ix, emb, _ := newCommitIndex(t, root, 10)
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	first := emb.callCount()
	stats, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 0 || stats.Unchanged != 2 {
		t.Errorf("segundo escaneo: embebidos %d, sin cambio %d", stats.Embedded, stats.Unchanged)
	}
	if emb.callCount() != first {
		t.Errorf("volvió a llamar al embebedor: %d contra %d", emb.callCount(), first)
	}
}

func TestCommitSyncIndexesOnlyTheNewOnes(t *testing.T) {
	// Al llegar un commit nuevo se embebe solo ese, no la historia completa.
	root := newGitRepo(t, [2]string{"uno", ""}, [2]string{"dos", ""})
	ix, _, _ := newCommitIndex(t, root, 10)
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	addCommit(t, root, "tres", "el nuevo")
	stats, err := ix.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 1 || stats.Unchanged != 2 {
		t.Errorf("embebidos %d y sin cambio %d; esperaba 1 y 2", stats.Embedded, stats.Unchanged)
	}
}

func TestCommitSyncDropsCommitsOutOfRange(t *testing.T) {
	// Lo que sale de la ventana —o desaparece por un rebase— se borra del
	// índice: un commit que ya no existe no debe salir en los resultados.
	root := newGitRepo(t, [2]string{"uno", ""}, [2]string{"dos", ""}, [2]string{"tres", ""})
	ix, emb, store := newCommitIndex(t, root, 3)
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	narrow := NewCommitIndex(root, store, emb, nil, 2)
	stats, err := narrow.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("se borraron %d commits, esperaba 1", stats.Removed)
	}
	if n, _ := narrow.Ready(); n != 2 {
		t.Errorf("quedaron %d commits indexados, esperaba 2", n)
	}
}

func TestCommitSearchFindsByMeaning(t *testing.T) {
	// La búsqueda devuelve el commit con su asunto legible. Con el embebedor
	// falso no se puede juzgar la relevancia, así que se comprueba la forma:
	// que devuelva resultados y que el asunto se recupere entero.
	root := newGitRepo(t, [2]string{"arregla el cobro duplicado", "la pasarela reintentaba"})
	ix, _, _ := newCommitIndex(t, root, 10)
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Search(context.Background(), "cobros repetidos", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("la búsqueda no devolvió nada")
	}
	if got[0].Subject() != "arregla el cobro duplicado" {
		t.Errorf("asunto = %q", got[0].Subject())
	}
	if len(got[0].Hash) < 8 {
		t.Errorf("hash incompleto: %q", got[0].Hash)
	}
	if !strings.Contains(got[0].Content, "la pasarela reintentaba") {
		t.Error("el contenido no trae el cuerpo del mensaje")
	}
}

func TestCommitSearchRejectsAnEmptyQuery(t *testing.T) {
	// Una consulta vacía es un error de quien llama, no una búsqueda que dé
	// cero resultados: embeberla gastaría cuota para nada.
	ix, _, _ := newCommitIndex(t, t.TempDir(), 10)
	if _, err := ix.Search(context.Background(), "   ", 5); err == nil {
		t.Error("aceptó una consulta vacía")
	}
}
