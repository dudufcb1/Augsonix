package codesearch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore guarda los vectores en Postgres con pgvector, para que el
// índice sobreviva a cambiar de máquina. Comparte base entre proyectos: cada
// fila lleva su workspace, así que un solo servidor atiende todos los repos sin
// que se mezclen entre sí.
type PostgresStore struct {
	pool      *pgxpool.Pool
	table     string
	workspace string
	model     string
	dims      int
}

// OpenPostgresStore conecta y deja el esquema listo. La tabla lleva la dimensión
// en el nombre porque el tipo vector la fija en la columna: así conviven índices
// de distinta dimensión en la misma base en vez de pelearse por una tabla.
func OpenPostgresStore(ctx context.Context, dsn, workspace, model string, dims int) (*PostgresStore, error) {
	if dims <= 0 {
		return nil, fmt.Errorf("codesearch: dimensión inválida %d", dims)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("codesearch: conectar a postgres: %w", err)
	}
	s := &PostgresStore{
		pool:      pool,
		table:     fmt.Sprintf("codesearch_chunks_%d", dims),
		workspace: workspace,
		model:     model,
		dims:      dims,
	}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// migrate crea tabla e índice si faltan. El índice HNSW se construye sobre un
// cast a halfvec porque el tipo vector solo se puede indexar hasta 2000
// dimensiones, y halfvec llega a 4000 con una pérdida de precisión que el
// reranker corrige de todos modos.
func (s *PostgresStore) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			workspace  text NOT NULL,
			path       text NOT NULL,
			chunk_hash text NOT NULL,
			start_line int  NOT NULL,
			end_line   int  NOT NULL,
			content    text NOT NULL,
			model      text NOT NULL,
			embedding  vector(%d) NOT NULL,
			PRIMARY KEY (workspace, path, chunk_hash)
		)`, s.table, s.dims),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_ws ON %s (workspace, path)`, s.table, s.table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_hnsw ON %s
			USING hnsw ((embedding::halfvec(%d)) halfvec_cosine_ops)`, s.table, s.table, s.dims),
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("codesearch: preparar esquema: %w", err)
		}
	}
	return nil
}

// Close suelta el pool de conexiones.
func (s *PostgresStore) Close() { s.pool.Close() }

// Dims es la dimensión de los vectores del índice.
func (s *PostgresStore) Dims() int { return s.dims }

// Replace deja el archivo con exactamente esos chunks, en una transacción: si
// se cayera a la mitad, el archivo quedaría sin sus vectores y la búsqueda
// dejaría de encontrarlo sin que nadie se entere.
func (s *PostgresStore) Replace(path string, chunks []Chunk, vecs []int8) error {
	if len(chunks)*s.dims != len(vecs) {
		return fmt.Errorf("codesearch: %d chunks piden %d valores, llegaron %d", len(chunks), len(chunks)*s.dims, len(vecs))
	}
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE workspace=$1 AND path=$2`, s.table), s.workspace, path); err != nil {
		return err
	}
	insert := fmt.Sprintf(`INSERT INTO %s
		(workspace, path, chunk_hash, start_line, end_line, content, model, embedding)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (workspace, path, chunk_hash) DO UPDATE
		SET start_line=EXCLUDED.start_line, end_line=EXCLUDED.end_line,
		    content=EXCLUDED.content, embedding=EXCLUDED.embedding`, s.table)
	for i, c := range chunks {
		vec := vecs[i*s.dims : (i+1)*s.dims]
		if _, err := tx.Exec(ctx, insert, s.workspace, c.Path, c.Hash,
			c.StartLine, c.EndLine, c.Content, s.model, encodeVector(vec)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Delete saca del índice todo lo que venía de un archivo.
func (s *PostgresStore) Delete(path string) {
	_, _ = s.pool.Exec(context.Background(),
		fmt.Sprintf(`DELETE FROM %s WHERE workspace=$1 AND path=$2`, s.table), s.workspace, path)
}

// Has reporta si un archivo tiene chunks en el índice.
func (s *PostgresStore) Has(path string) bool {
	var ok bool
	err := s.pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE workspace=$1 AND path=$2)`, s.table),
		s.workspace, path).Scan(&ok)
	return err == nil && ok
}

// Paths devuelve los archivos indexados, ordenados.
func (s *PostgresStore) Paths() []string {
	rows, err := s.pool.Query(context.Background(),
		fmt.Sprintf(`SELECT DISTINCT path FROM %s WHERE workspace=$1 ORDER BY path`, s.table), s.workspace)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Stats reporta cuántos archivos y chunks hay indexados.
func (s *PostgresStore) Stats() (files, chunks int) {
	_ = s.pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT COUNT(DISTINCT path), COUNT(*) FROM %s WHERE workspace=$1`, s.table),
		s.workspace).Scan(&files, &chunks)
	return files, chunks
}

// Search devuelve los chunks más parecidos. El orden lo resuelve el índice HNSW
// sobre el mismo cast a halfvec con que se construyó; sin ese cast Postgres
// ignoraría el índice y haría un barrido secuencial.
func (s *PostgresStore) Search(query []int8, limit int) []Match {
	if len(query) != s.dims || limit <= 0 {
		return nil
	}
	q := fmt.Sprintf(`SELECT path, start_line, end_line, content, chunk_hash,
		1 - (embedding::halfvec(%d) <=> $1::halfvec(%d)) AS score
		FROM %s WHERE workspace=$2 AND model=$3
		ORDER BY embedding::halfvec(%d) <=> $1::halfvec(%d)
		LIMIT $4`, s.dims, s.dims, s.table, s.dims, s.dims)

	rows, err := s.pool.Query(context.Background(), q, encodeVector(query), s.workspace, s.model, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.Chunk.Path, &m.Chunk.StartLine, &m.Chunk.EndLine,
			&m.Chunk.Content, &m.Chunk.Hash, &m.Score); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// Save no hace nada: cada Replace ya quedó confirmado en su transacción. Existe
// para cumplir la interfaz, que el almacén local sí necesita para volcar a disco.
func (s *PostgresStore) Save() error { return nil }

// encodeVector arma el literal que entiende pgvector: "[1,2,3]".
func encodeVector(v []int8) string {
	var b strings.Builder
	b.Grow(len(v)*4 + 2)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(int(x)))
	}
	b.WriteByte(']')
	return b.String()
}

var _ VectorStore = (*PostgresStore)(nil)
