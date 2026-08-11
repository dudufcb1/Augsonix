package codesearch

// VectorStore es donde viven los vectores del índice. Es interfaz porque el
// almacenamiento es lo que más cambia según el despliegue: uno local no pide
// infraestructura pero no sobrevive a cambiar de máquina; uno remoto sí, a
// cambio de operar un servidor. Las rutas son relativas a la raíz del
// workspace y siempre con separador "/", para que el índice sea portable.
type VectorStore interface {
	// Replace deja el archivo con exactamente esos chunks y vectores. vecs
	// viene concatenado: Dims() valores por chunk.
	Replace(path string, chunks []Chunk, vecs []int8) error
	// Delete saca del índice todo lo que venía de un archivo.
	Delete(path string)
	// Has reporta si un archivo tiene chunks en el índice.
	Has(path string) bool
	// Paths devuelve los archivos indexados, ordenados.
	Paths() []string
	// Search devuelve los limit chunks más parecidos, de mayor a menor
	// similitud coseno.
	Search(query []int8, limit int) []Match
	// Stats reporta cuántos archivos y chunks hay indexados.
	Stats() (files, chunks int)
	// Save persiste lo acumulado. Se llama al cerrar un lote, no por archivo.
	Save() error
	// Dims es la dimensión de los vectores que acepta.
	Dims() int
}
