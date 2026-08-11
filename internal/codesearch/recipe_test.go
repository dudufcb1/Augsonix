package codesearch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// chunkingFingerprint resume el resultado del troceo sobre las muestras: si
// cambia la forma de cortar, cambia la huella.
func chunkingFingerprint() string {
	samples := map[string]string{
		"a.go": goSample, "a.py": pythonSample, "a.php": phpSample, "a.ts": tsSample,
		"h.py": hostilePython, "h.js": hostileJS, "h.php": hostilePHP,
		// Un archivo por encima del techo ejercita el empaquetado, que es lo
		// que reacciona a un cambio de tamaño máximo.
		"big.go": packingSample(),
		"big.py": packingSamplePython(),
	}
	paths := make([]string, 0, len(samples))
	for p := range samples {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		// Las fronteras entran aparte de los bloques: un cambio en la detección
		// que el empaquetado alcance a absorber sigue siendo un cambio, y en
		// otro archivo sí movería los cortes.
		for _, b := range boundaries(p, samples[p]) {
			fmt.Fprintf(h, "b %s:%d-%d", p, b.Start, b.End)
			for _, sub := range b.Sub {
				fmt.Fprintf(h, "|%d-%d", sub.Start, sub.End)
			}
			fmt.Fprintln(h)
		}
		for _, c := range ChunkFile(p, samples[p]) {
			fmt.Fprintf(h, "c %s:%d-%d:%s\n", c.Path, c.StartLine, c.EndLine, c.Hash)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestChunkingIsStable(t *testing.T) {
	// Este test falla cuando cambia el resultado del troceo. Si el cambio es
	// deliberado, sube chunkerVersion en recipe.go y actualiza la huella de
	// abajo. Sin ese paso, los índices ya construidos se quedan con el troceo
	// viejo para siempre: el hash de archivo seguiría coincidiendo y nadie
	// volvería a mirar esos archivos.
	const want = "998ffe383f41a2ad185c1e2fa4f2a059a64035e429c64a663e2123903e41cbb1"
	if got := chunkingFingerprint(); got != want {
		t.Fatalf("el troceo cambió.\n  huella: %s\n  esperada: %s\n"+
			"Si el cambio es deliberado: sube chunkerVersion y pon esta huella aquí.", got, want)
	}
}

func TestFileHashDependsOnRecipe(t *testing.T) {
	// El mismo archivo bajo recetas distintas da hashes distintos: de eso
	// depende que cambiar el troceo o el modelo dispare la reindexación en vez
	// de dejar el índice viejo sin que nadie se entere.
	content := "package main\n\nfunc main() {}\n"
	a := FileHash(IndexRecipe("voyage-code-3", 2048), content)
	b := FileHash(IndexRecipe("voyage-code-3", 1024), content)
	c := FileHash(IndexRecipe("voyage-3-large", 2048), content)
	if a == b {
		t.Error("cambiar la dimensión no cambió el hash")
	}
	if a == c {
		t.Error("cambiar el modelo no cambió el hash")
	}
	if a != FileHash(IndexRecipe("voyage-code-3", 2048), content) {
		t.Error("la misma receta y el mismo contenido dieron hashes distintos")
	}
}

func TestIndexRecipeCarriesTheChunkerVersion(t *testing.T) {
	// La receta debe nombrar la versión del troceo: es la mitad que no depende
	// del proveedor de embeddings y la que más se toca.
	got := IndexRecipe("voyage-code-3", 2048)
	if !strings.Contains(got, fmt.Sprintf("chunker=%d", chunkerVersion)) {
		t.Errorf("la receta %q no lleva la versión del troceo", got)
	}
}

// packingSample arma un archivo Go con funciones de largo variable, para que la
// huella reaccione a cualquier cambio en cómo se agrupan.
func packingSample() string {
	var b strings.Builder
	b.WriteString("package packing\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "\n// Paso%d documenta el paso %d del proceso.\nfunc Paso%d(entrada int) int {\n", i, i, i)
		for j := 0; j <= i%7; j++ {
			fmt.Fprintf(&b, "\tacumulado := entrada*%d + %d\n\tentrada = acumulado\n", j+1, i)
		}
		b.WriteString("\treturn entrada\n}\n")
	}
	return b.String()
}

// packingSamplePython es el equivalente para el detector por sangría, que sigue
// un camino distinto al de llaves.
func packingSamplePython() string {
	var b strings.Builder
	b.WriteString("import os\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "\n\ndef paso_%d(entrada):\n    \"\"\"Paso %d del proceso.\"\"\"\n", i, i)
		for j := 0; j <= i%7; j++ {
			fmt.Fprintf(&b, "    acumulado = entrada * %d + %d\n    entrada = acumulado\n", j+1, i)
		}
		b.WriteString("    return entrada\n")
	}
	return b.String()
}
