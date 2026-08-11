package codesearch

import (
	"context"
	"path/filepath"
	"testing"
)

// indexOn arma un índice sobre un workspace y un directorio de índice dados,
// para poder abrir dos veces lo mismo con embebedores distintos.
func indexOn(t *testing.T, root, indexDir, model string) (*Index, *fakeEmbedder) {
	t.Helper()
	emb := &fakeEmbedder{dims: 8, model: model}
	store, err := OpenLocalStore(indexDir, model, 8)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	return NewIndex(root, store, state, emb, nil), emb
}

func TestSyncSkipsWorkAlreadyDoneWithTheSameRecipe(t *testing.T) {
	// La base de lo demás: dos escaneos seguidos sin cambios no vuelven a
	// embeber nada. Si esto fallara, el índice se recalcularía en cada arranque
	// y la cuota se iría en trabajo repetido.
	root, indexDir := t.TempDir(), filepath.Join(t.TempDir(), "idx")
	writeFile(t, root, "a.go", body("alpha"))
	ix, emb := indexOn(t, root, indexDir, "fake")
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	first := emb.callCount()
	if first == 0 {
		t.Fatal("el primer escaneo no embebió nada")
	}
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if emb.callCount() != first {
		t.Errorf("el segundo escaneo volvió a embeber: %d llamadas contra %d", emb.callCount(), first)
	}
}

func TestSyncReindexesWhenTheRecipeChanges(t *testing.T) {
	// Cambiar el modelo —o el troceo, que viaja en la misma receta— deja lo
	// indexado describiendo algo que ya no se produce. El escaneo tiene que
	// notarlo solo: un índice viejo no avisa de que lo está.
	root, indexDir := t.TempDir(), filepath.Join(t.TempDir(), "idx")
	writeFile(t, root, "a.go", body("alpha"))
	writeFile(t, root, "b.ts", body("beta"))

	ix, emb := indexOn(t, root, indexDir, "fake")
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if emb.callCount() == 0 {
		t.Fatal("el escaneo inicial no embebió nada")
	}

	// Mismo workspace y mismo directorio de índice, otro modelo.
	other, otherEmb := indexOn(t, root, indexDir, "otro-modelo")
	stats, err := other.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 2 {
		t.Errorf("se reindexaron %d archivos, esperaba los 2", stats.Embedded)
	}
	if stats.Unchanged != 0 {
		t.Errorf("%d archivos se dieron por buenos con la receta vieja", stats.Unchanged)
	}
	if otherEmb.callCount() == 0 {
		t.Error("no se llamó al embebedor nuevo")
	}
}

func TestSyncKeepsWorkWhenOnlyTheWorkspaceMoves(t *testing.T) {
	// El caso contrario: abrir el mismo proyecto otra vez con la misma receta
	// no debe reindexar. Es lo que hace que arrancar sea barato.
	root, indexDir := t.TempDir(), filepath.Join(t.TempDir(), "idx")
	writeFile(t, root, "a.go", body("alpha"))
	ix, _ := indexOn(t, root, indexDir, "fake")
	if _, err := ix.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	again, _ := indexOn(t, root, indexDir, "fake")
	stats, err := again.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 0 {
		t.Errorf("reindexó %d archivos sin que cambiara nada", stats.Embedded)
	}
}
