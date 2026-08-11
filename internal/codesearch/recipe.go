package codesearch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// chunkerVersion identifica cómo se trocea. Sube cuando cambie el resultado del
// troceo: es lo que hace que un índice construido con la versión anterior se
// reconstruya solo en vez de quedarse viejo en silencio. TestChunkingIsStable
// falla si el troceo cambia sin subirla.
const chunkerVersion = 2

// IndexRecipe describe cómo se construyó una entrada del índice: con qué troceo
// y con qué modelo de embeddings. Va dentro del hash de cada archivo, así que
// cambiar cualquiera de los dos invalida lo guardado archivo por archivo, sin
// necesidad de borrar el índice ni de que nadie se acuerde de hacerlo.
func IndexRecipe(model string, dims int) string {
	return fmt.Sprintf("chunker=%d/model=%s/dims=%d", chunkerVersion, model, dims)
}

// FileHash identifica el contenido de un archivo bajo una receta. Dos archivos
// idénticos indexados con recetas distintas dan hashes distintos, que es lo que
// dispara la reindexación.
func FileHash(recipe, content string) string {
	sum := sha256.Sum256([]byte(recipe + "\x00" + content))
	return hex.EncodeToString(sum[:])
}
