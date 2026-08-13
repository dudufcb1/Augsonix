package config

import (
	"fmt"
	"reflect"
	"strings"
)

// renderCodeSearch escribe la sección [codesearch] del config de usuario. Sin
// ella, el guardado canónico borraba la sección y apagaba la búsqueda semántica
// sin avisar. Se omite entera cuando todo está en su valor por defecto.
func renderCodeSearch(b *strings.Builder, c CodeSearchConfig) {
	if reflect.DeepEqual(c, DefaultCodeSearch()) {
		return
	}
	b.WriteString("\n[codesearch]\n")
	b.WriteString("# Búsqueda semántica sobre el código del proyecto. Se administra con:\n")
	b.WriteString("#   reasonix codesearch status | list | reindex | clear\n")
	fmt.Fprintf(b, "enabled = %t\n", c.Enabled)
	// Cada clave se escribe aunque coincida con el default: al recargar, una
	// clave ausente vuelve al cero del struct, no al default, y eso deja el
	// índice inalcanzable (backend y postgres_url_env vacíos).
	fmt.Fprintf(b, "model = %q   # modelo de embeddings; cambiarlo invalida el índice\n", c.Model)
	fmt.Fprintf(b, "dimensions = %d   # se puede bajar sin reindexar, no subir\n", c.Dimensions)
	fmt.Fprintf(b, "rerank_model = %q   # vacío desactiva el rerank\n", c.RerankModel)
	fmt.Fprintf(b, "api_key_env = %q   # la llave nunca va en este archivo\n", c.APIKeyEnv)
	if strings.TrimSpace(c.BaseURL) != "" {
		fmt.Fprintf(b, "base_url = %q   # endpoint alterno compatible\n", c.BaseURL)
	}
	fmt.Fprintf(b, "backend = %q   # dónde vive el índice\n", c.Backend)
	if strings.TrimSpace(c.PostgresURLEnv) != "" {
		fmt.Fprintf(b, "postgres_url_env = %q   # la cadena de conexión nunca va aquí\n", c.PostgresURLEnv)
	}
	fmt.Fprintf(b, "prompt_mode = %q   # cuánto empuja el prompt a buscar por significado\n", c.Prompt)
	fmt.Fprintf(b, "auto_index = %t   # sincroniza el índice al abrir el workspace\n", c.AutoIndex)
	fmt.Fprintf(b, "watch = %t   # mantiene el índice al día mientras se trabaja\n", c.Watch)
	fmt.Fprintf(b, "commits = %t   # indexa también la historia del repositorio\n", c.Commits)
	fmt.Fprintf(b, "commit_limit = %d   # cuántos commits entran al índice\n", c.CommitLimit)
	fmt.Fprintf(b, "grep_friction = %q   # qué hace tras varios greps seguidos\n", c.GrepFriction)
	fmt.Fprintf(b, "grep_friction_limit = %d   # cuántos greps seguidos disparan el aviso\n", c.GrepFrictionLimit)
	if len(c.Containers) > 0 {
		b.WriteString("# Carpetas que agrupan proyectos: no se indexan enteras, sus subcarpetas sí.\n")
		fmt.Fprintf(b, "containers = %s\n", renderStringList(c.Containers))
	}
}

func renderStringList(values []string) string {
	quotedValues := make([]string, len(values))
	for i, value := range values {
		quotedValues[i] = fmt.Sprintf("%q", value)
	}
	return "[" + strings.Join(quotedValues, ", ") + "]"
}
