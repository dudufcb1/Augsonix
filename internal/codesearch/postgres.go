package codesearch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
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
	// name es la carpeta del proyecto, guardada solo para que el listado sea
	// legible: un identificador de dieciséis hexadecimales no le dice a nadie
	// qué índice puede borrar.
	name  string
	model string
	dims  int
}

// OpenPostgresStore conecta y deja el esquema listo. La tabla lleva la dimensión
// en el nombre porque el tipo vector la fija en la columna: así conviven índices
// de distinta dimensión en la misma base en vez de pelearse por una tabla.
func OpenPostgresStore(ctx context.Context, dsn, workspace, name, model string, dims int) (*PostgresStore, error) {
	if dims <= 0 {
		return nil, fmt.Errorf("codesearch: dimensión inválida %d", dims)
	}
	// El pool tiene que dar al menos una conexión por archivo en vuelo: con
	// menos, la mitad de los trabajadores espera turno y el paralelismo no sirve.
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("codesearch: dsn de postgres inválido: %w", err)
	}
	if poolCfg.MaxConns < syncWorkers+1 {
		poolCfg.MaxConns = syncWorkers + 1
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("codesearch: conectar a postgres: %w", err)
	}
	s := &PostgresStore{
		pool:      pool,
		table:     fmt.Sprintf("codesearch_chunks_%d", dims),
		workspace: workspace,
		name:      name,
		model:     model,
		dims:      dims,
	}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	// Rellenar el nombre de lo indexado antes de que existiera la columna, para
	// que el listado sea legible sin esperar a que el proyecto se reindexe.
	if name != "" {
		_, _ = pool.Exec(ctx,
			fmt.Sprintf(`UPDATE %s SET ws_name=$1 WHERE workspace=$2 AND ws_name=''`, s.table),
			name, workspace)
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
			file_hash  text NOT NULL DEFAULT '',
			ws_name    text NOT NULL DEFAULT '',
			embedding  vector(%d) NOT NULL,
			PRIMARY KEY (workspace, path, chunk_hash)
		)`, s.table, s.dims),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS file_hash text NOT NULL DEFAULT ''`, s.table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS ws_name text NOT NULL DEFAULT ''`, s.table),
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
func (s *PostgresStore) Replace(path, fileHash string, chunks []Chunk, vecs []int8) error {
	if len(chunks)*s.dims != len(vecs) {
		return fmt.Errorf("codesearch: %d chunks piden %d valores, llegaron %d", len(chunks), len(chunks)*s.dims, len(vecs))
	}
	// Todo en un solo envío: pgx las encola y las manda juntas, en una
	// transacción implícita. Por separado costarían un viaje de ida y vuelta
	// cada una, y con el almacén en otra región la latencia es lo que domina.
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf(`DELETE FROM %s WHERE workspace=$1 AND path=$2`, s.table), s.workspace, path)
	for _, group := range groupChunks(chunks, s.dims, vecs) {
		batch.Queue(s.insertStatement(len(group.chunks)), s.insertArgs(path, fileHash, group)...)
	}
	results := s.pool.SendBatch(context.Background(), batch)
	return results.Close()
}

// FileHash devuelve con qué contenido se indexó un archivo. Vive en la base y
// no en un archivo local: así reanudar una indexación interrumpida, o abrir el
// repositorio en otra máquina, no vuelve a embeber lo que ya está guardado.
func (s *PostgresStore) FileHash(path string) (string, bool) {
	var h string
	err := s.pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT file_hash FROM %s WHERE workspace=$1 AND path=$2 LIMIT 1`, s.table),
		s.workspace, path).Scan(&h)
	if err != nil || h == "" {
		return "", false
	}
	return h, true
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

// WorkspaceIndex resume lo que un proyecto ocupa en la base compartida.
type WorkspaceIndex struct {
	Workspace string
	Name      string
	Files     int
	Chunks    int
}

// Workspaces enumera todo lo indexado en la base, no solo el proyecto actual.
// Es la única forma de encontrar índices de proyectos que ya no se tocan: sin
// esto se quedan ocupando espacio sin que nadie sepa que están ahí.
func (s *PostgresStore) Workspaces() ([]WorkspaceIndex, error) {
	rows, err := s.pool.Query(context.Background(),
		fmt.Sprintf(`SELECT workspace, COALESCE(MAX(ws_name),''), COUNT(DISTINCT path), COUNT(*)
			FROM %s GROUP BY workspace ORDER BY 2, 1`, s.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceIndex
	for rows.Next() {
		var w WorkspaceIndex
		if err := rows.Scan(&w.Workspace, &w.Name, &w.Files, &w.Chunks); err != nil {
			continue
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWorkspace borra el índice completo de un proyecto de la base.
func (s *PostgresStore) DeleteWorkspace(workspace string) (int64, error) {
	tag, err := s.pool.Exec(context.Background(),
		fmt.Sprintf(`DELETE FROM %s WHERE workspace=$1`, s.table), workspace)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// maxInsertRows acota cuántas filas caben en una sentencia. Postgres admite
// 65535 parámetros y cada fila usa insertColumns, así que el tope real queda
// muy por encima; el límite existe para que un archivo desmedido no arme una
// sentencia de megabytes.
const maxInsertRows = 200

// insertColumns son las columnas que escribe cada fila, en el orden en que
// insertArgs las emite.
const insertColumns = 10

// chunkGroup es un tramo de fragmentos con sus vectores, listo para una sola
// sentencia.
type chunkGroup struct {
	chunks []Chunk
	vecs   []int8
}

// groupChunks reparte los fragmentos en tandas que quepan en una sentencia.
// Escribirlos de uno en uno cuesta un viaje a la base por fragmento, y con el
// almacén en otra región la latencia de ida y vuelta domina el indexado.
func groupChunks(chunks []Chunk, dims int, vecs []int8) []chunkGroup {
	var out []chunkGroup
	for start := 0; start < len(chunks); start += maxInsertRows {
		end := min(start+maxInsertRows, len(chunks))
		out = append(out, chunkGroup{
			chunks: chunks[start:end],
			vecs:   vecs[start*dims : end*dims],
		})
	}
	return out
}

// insertStatement arma un INSERT con tantas tuplas de VALUES como filas.
func (s *PostgresStore) insertStatement(rows int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `INSERT INTO %s
		(workspace, path, chunk_hash, start_line, end_line, content, model, embedding, file_hash, ws_name)
		VALUES `, s.table)
	for i := range rows {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(")
		for c := range insertColumns {
			if c > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "$%d", i*insertColumns+c+1)
		}
		b.WriteString(")")
	}
	b.WriteString(` ON CONFLICT (workspace, path, chunk_hash) DO UPDATE
		SET start_line=EXCLUDED.start_line, end_line=EXCLUDED.end_line,
		    content=EXCLUDED.content, embedding=EXCLUDED.embedding,
		    file_hash=EXCLUDED.file_hash, ws_name=EXCLUDED.ws_name`)
	return b.String()
}

// insertArgs aplana los argumentos de todas las filas en el orden que espera
// insertStatement.
func (s *PostgresStore) insertArgs(path, fileHash string, g chunkGroup) []any {
	args := make([]any, 0, len(g.chunks)*insertColumns)
	for i, c := range g.chunks {
		vec := g.vecs[i*s.dims : (i+1)*s.dims]
		args = append(args, s.workspace, path, c.Hash, c.StartLine, c.EndLine,
			c.Content, s.model, encodeVector(vec), fileHash, s.name)
	}
	return args
}
