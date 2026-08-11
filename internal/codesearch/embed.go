package codesearch

import "context"

// InputKind distingue si un texto se embebe como consulta o como documento.
// No es cosmético: el proveedor antepone instrucciones distintas en cada caso,
// y embeber la consulta como documento degrada la recuperación.
type InputKind string

const (
	KindQuery    InputKind = "query"
	KindDocument InputKind = "document"
)

// Embedder convierte texto en vectores. Devuelve int8 porque es lo que guarda
// el índice; un proveedor que entregue float cuantiza antes de responder.
type Embedder interface {
	// Embed devuelve un vector por texto, en el mismo orden que la entrada.
	Embed(ctx context.Context, texts []string, kind InputKind) ([][]int8, error)
	// Dims es la dimensión de los vectores que produce.
	Dims() int
	// Model identifica el modelo, para invalidar el índice si cambia.
	Model() string
}

// Reranker reordena candidatos leyendo consulta y documento juntos, que es algo
// que un embedding no puede hacer: los vectores se calculan por separado y sin
// saber qué se preguntó. Es la etapa que arregla el orden fino.
type Reranker interface {
	// Rerank devuelve los índices de docs ordenados por relevancia, recortados
	// a topK. Los índices apuntan al slice docs recibido.
	Rerank(ctx context.Context, query string, docs []string, topK int) ([]Ranked, error)
}

// Ranked es un documento reordenado: su posición en la entrada y su relevancia.
type Ranked struct {
	Index int
	Score float32
}
