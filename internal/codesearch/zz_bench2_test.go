package codesearch

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Medición temporal: escritura agrupada contra la base real.
func TestBenchReplace(t *testing.T) {
	dsn := os.Getenv("CODESEARCH_POSTGRES_URL")
	if dsn == "" {
		t.Skip("sin DSN")
	}
	ctx := context.Background()
	st, err := OpenPostgresStore(ctx, dsn, "bench-tmp", "bench", "m", 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 5, 20} {
		chunks := make([]Chunk, n)
		vecs := make([]int8, n*2048)
		for i := range chunks {
			chunks[i] = Chunk{Path: "x.go", Hash: fmt.Sprintf("h%d", i), StartLine: i, EndLine: i + 1, Content: "contenido de prueba"}
		}
		var total time.Duration
		const rounds = 3
		for i := 0; i < rounds; i++ {
			start := time.Now()
			if err := st.Replace(fmt.Sprintf("f%d.go", i), "fh", chunks, vecs); err != nil {
				t.Fatal(err)
			}
			total += time.Since(start)
		}
		fmt.Printf("Replace de %2d fragmentos: %v\n", n, (total / rounds).Round(time.Millisecond))
		for i := 0; i < rounds; i++ {
			st.Delete(fmt.Sprintf("f%d.go", i))
		}
	}
}
