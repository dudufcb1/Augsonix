package codesearch

import (
	"strings"
	"testing"
)

func TestInsertStatementHasOnePlaceholderPerValue(t *testing.T) {
	// Un desfase entre los marcadores de la sentencia y los argumentos que se
	// mandan haría que cada fragmento se guardara con los datos de otro, sin
	// que la base se queje.
	s := &PostgresStore{table: "t"}
	for _, rows := range []int{1, 2, 7} {
		stmt := s.insertStatement(rows)
		want := rows * insertColumns
		if got := strings.Count(stmt, "$"); got != want {
			t.Errorf("%d filas: %d marcadores, esperaba %d", rows, got, want)
		}
		if got := strings.Count(stmt, "),("); got != rows-1 {
			t.Errorf("%d filas: %d separadores entre tuplas", rows, got)
		}
		if !strings.Contains(stmt, "ON CONFLICT") {
			t.Errorf("%d filas: se perdió el ON CONFLICT", rows)
		}
	}
}

func TestInsertArgsKeepEachChunkWithItsVector(t *testing.T) {
	// El vector de cada fragmento se recorta del bloque plano por posición. Si
	// el recorte se corriera, un fragmento quedaría indexado con el vector de
	// otro y la búsqueda devolvería el archivo equivocado.
	s := &PostgresStore{workspace: "ws", model: "m", name: "n", dims: 2}
	chunks := []Chunk{
		{Hash: "h1", StartLine: 1, EndLine: 2, Content: "primero"},
		{Hash: "h2", StartLine: 3, EndLine: 4, Content: "segundo"},
	}
	vecs := []int8{10, 11, 20, 21}
	args := s.insertArgs("a.go", "fh", chunkGroup{chunks: chunks, vecs: vecs})

	if len(args) != len(chunks)*insertColumns {
		t.Fatalf("%d argumentos para %d fragmentos", len(args), len(chunks))
	}
	// Columna 8 (índice 7) es el vector codificado.
	if got := args[7].(string); got != encodeVector([]int8{10, 11}) {
		t.Errorf("vector del primer fragmento = %s", got)
	}
	if got := args[insertColumns+7].(string); got != encodeVector([]int8{20, 21}) {
		t.Errorf("vector del segundo fragmento = %s", got)
	}
	if args[2] != "h1" || args[insertColumns+2] != "h2" {
		t.Errorf("los hashes no corresponden: %v y %v", args[2], args[insertColumns+2])
	}
	if args[5] != "primero" || args[insertColumns+5] != "segundo" {
		t.Errorf("los contenidos no corresponden")
	}
}

func TestGroupChunksSplitsOversizedFiles(t *testing.T) {
	// Un archivo con muchísimos fragmentos no puede ir en una sola sentencia:
	// Postgres tiene un tope de parámetros. Las tandas deben cubrir todos los
	// fragmentos, en orden y sin repetir.
	const dims = 2
	n := maxInsertRows*2 + 3
	chunks := make([]Chunk, n)
	vecs := make([]int8, n*dims)
	for i := range chunks {
		chunks[i] = Chunk{Hash: string(rune('a' + i%26))}
		vecs[i*dims] = int8(i % 100)
	}
	groups := groupChunks(chunks, dims, vecs)
	if len(groups) != 3 {
		t.Fatalf("se armaron %d tandas para %d fragmentos", len(groups), n)
	}
	total := 0
	for _, g := range groups {
		if len(g.chunks) > maxInsertRows {
			t.Errorf("una tanda lleva %d filas, tope %d", len(g.chunks), maxInsertRows)
		}
		if len(g.vecs) != len(g.chunks)*dims {
			t.Errorf("tanda con %d fragmentos y %d valores de vector", len(g.chunks), len(g.vecs))
		}
		total += len(g.chunks)
	}
	if total != n {
		t.Errorf("las tandas cubren %d fragmentos de %d", total, n)
	}
	// El primer valor del vector identifica la posición original.
	if groups[1].vecs[0] != int8(maxInsertRows%100) {
		t.Errorf("la segunda tanda no arranca donde terminó la primera")
	}
}

func TestGroupChunksHandlesASingleChunk(t *testing.T) {
	// El caso común: un archivo chico produce una sola tanda.
	groups := groupChunks([]Chunk{{Hash: "h"}}, 3, []int8{1, 2, 3})
	if len(groups) != 1 || len(groups[0].chunks) != 1 {
		t.Fatalf("se armaron %d tandas", len(groups))
	}
}
