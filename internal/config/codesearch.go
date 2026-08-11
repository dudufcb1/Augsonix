package config

import (
	"fmt"
	"strings"
)

// CodeSearchPromptMode decide cuánta instrucción sobre la búsqueda semántica
// entra al prompt del sistema. Existe como perilla porque el modo correcto se
// mide, no se adivina: más instrucción sube el uso de la herramienta pero cuesta
// tokens fijos en cada turno de cada sesión.
type CodeSearchPromptMode string

const (
	// PromptModeTool no toca el prompt: solo la descripción de la herramienta.
	// Es el más barato y deja al modelo decidir.
	PromptModeTool CodeSearchPromptMode = "tool"
	// PromptModeEncourage añade una línea sugiriendo consultarla al explorar.
	PromptModeEncourage CodeSearchPromptMode = "encourage"
	// PromptModeMandatory exige consultarla antes de editar, pidiendo todos los
	// símbolos en una sola llamada. Es lo que hace que el modelo la use de
	// verdad, y también lo que más cuesta.
	PromptModeMandatory CodeSearchPromptMode = "mandatory"
)

// GrepFriction empuja del grep encadenado hacia la búsqueda semántica sin
// quitarle la herramienta al modelo. Cuál conviene se mide, igual que el modo de
// prompt, y por eso es una perilla y no una decisión tomada.
type GrepFriction string

const (
	// FrictionOff deja grep como siempre.
	FrictionOff GrepFriction = "off"
	// FrictionSemi pide declarar no_semantic_needed para seguir con grep.
	FrictionSemi GrepFriction = "semi"
	// FrictionStrict corta y manda a code_search.
	FrictionStrict GrepFriction = "strict"
)

// CodeSearchBackend es dónde viven los vectores.
type CodeSearchBackend string

const (
	// BackendLocal guarda el índice en el workspace. Sin infraestructura, pero
	// no viaja a otra máquina.
	BackendLocal CodeSearchBackend = "local"
	// BackendPostgres usa Postgres con pgvector, para compartir el índice entre
	// máquinas.
	BackendPostgres CodeSearchBackend = "postgres"
)

// CodeSearchConfig configura el índice semántico del workspace. Con Enabled en
// false la herramienta no se registra y no cuesta nada en el prompt.
type CodeSearchConfig struct {
	Enabled bool `toml:"enabled"`
	// Model es el modelo de embeddings. Cambiarlo invalida el índice: vectores
	// de modelos distintos no son comparables.
	Model string `toml:"model"`
	// Dimensions debe estar entre las que acepta el modelo. Cambiarla invalida
	// el índice, y por Matryoshka se puede bajar después sin reindexar pero no
	// subir: por eso el valor por defecto es el mayor que acepta el modelo.
	Dimensions int `toml:"dimensions"`
	// RerankModel reordena los candidatos leyendo consulta y código juntos.
	// Vacío desactiva el rerank y deja el orden del coseno.
	RerankModel string `toml:"rerank_model"`
	// APIKeyEnv es la variable de entorno con la credencial del proveedor. La
	// llave nunca va en el archivo de configuración.
	APIKeyEnv string `toml:"api_key_env"`
	// BaseURL apunta a otro endpoint compatible; vacío usa el del proveedor.
	BaseURL string               `toml:"base_url"`
	Backend CodeSearchBackend    `toml:"backend"`
	Prompt  CodeSearchPromptMode `toml:"prompt_mode"`
	// PostgresURLEnv es la variable con la cadena de conexión cuando el backend
	// es postgres. Igual que la llave, nunca en el archivo.
	PostgresURLEnv string `toml:"postgres_url_env"`
	// AutoIndex sincroniza el índice al abrir el workspace, en segundo plano.
	AutoIndex bool `toml:"auto_index"`
	// Watch mantiene el índice al día mientras se trabaja, sin esperar a la
	// siguiente sesión.
	Watch bool `toml:"watch"`
	// Commits indexa además la historia del repositorio, para poder buscar cómo
	// se hizo antes un cambio parecido. Va aparte porque cuesta cuota propia y
	// no todos los proyectos la necesitan.
	Commits bool `toml:"commits"`
	// CommitLimit acota cuántos commits se indexan hacia atrás. Cero toma el
	// valor por defecto del indexador.
	CommitLimit int `toml:"commit_limit"`
	// GrepFriction interviene cuando el modelo encadena búsquedas de texto sin
	// consultar el índice.
	GrepFriction GrepFriction `toml:"grep_friction"`
	// GrepFrictionLimit son las búsquedas de texto seguidas que se toleran.
	GrepFrictionLimit int `toml:"grep_friction_limit"`
}

// DefaultCodeSearch son los valores con los que arranca la función. Está
// deshabilitada por defecto porque indexar cuesta dinero del usuario.
func DefaultCodeSearch() CodeSearchConfig {
	return CodeSearchConfig{
		Enabled:           false,
		Model:             "voyage-code-4",
		Dimensions:        1024,
		RerankModel:       "rerank-2.5",
		APIKeyEnv:         "VOYAGE_API_KEY",
		Backend:           BackendLocal,
		Prompt:            PromptModeTool,
		PostgresURLEnv:    "CODESEARCH_POSTGRES_URL",
		AutoIndex:         true,
		Watch:             true,
		GrepFriction:      FrictionOff,
		GrepFrictionLimit: 3,
	}
}

// Warnings enumera los valores que no se reconocieron. Normalized los corrige
// en silencio para que el índice siga usable, y callarse ahí es peligroso: un
// grep_friction = "true" deja la función apagada mientras el usuario cree que
// la activó, y nada se lo dice.
func (c CodeSearchConfig) Warnings() []string {
	var out []string
	switch c.Backend {
	case "", BackendLocal, BackendPostgres:
	default:
		out = append(out, fmt.Sprintf("[codesearch] backend %q no existe; usando %q (válidos: local, postgres)", c.Backend, BackendLocal))
	}
	switch c.Prompt {
	case "", PromptModeTool, PromptModeEncourage, PromptModeMandatory:
	default:
		out = append(out, fmt.Sprintf("[codesearch] prompt_mode %q no existe; usando %q (válidos: tool, encourage, mandatory)", c.Prompt, PromptModeTool))
	}
	switch c.GrepFriction {
	case "", FrictionOff, FrictionSemi, FrictionStrict:
	default:
		out = append(out, fmt.Sprintf("[codesearch] grep_friction %q no existe; usando %q (válidos: off, semi, strict)", c.GrepFriction, FrictionOff))
	}
	return out
}

// Normalized rellena los huecos con los valores por defecto y corrige lo que no
// reconoce, para que un archivo a medio escribir no deje el índice inservible.
// Lo corregido se reporta aparte con Warnings.
func (c CodeSearchConfig) Normalized() CodeSearchConfig {
	d := DefaultCodeSearch()
	if strings.TrimSpace(c.Model) == "" {
		c.Model = d.Model
	}
	if c.Dimensions <= 0 {
		c.Dimensions = d.Dimensions
	}
	if strings.TrimSpace(c.APIKeyEnv) == "" {
		c.APIKeyEnv = d.APIKeyEnv
	}
	switch c.Backend {
	case BackendLocal, BackendPostgres:
	default:
		c.Backend = d.Backend
	}
	switch c.Prompt {
	case PromptModeTool, PromptModeEncourage, PromptModeMandatory:
	default:
		c.Prompt = d.Prompt
	}
	switch c.GrepFriction {
	case FrictionOff, FrictionSemi, FrictionStrict:
	default:
		c.GrepFriction = d.GrepFriction
	}
	if c.GrepFrictionLimit <= 0 {
		c.GrepFrictionLimit = d.GrepFrictionLimit
	}
	if strings.TrimSpace(c.PostgresURLEnv) == "" {
		c.PostgresURLEnv = d.PostgresURLEnv
	}
	return c
}

// PromptGuidance es el texto que el modo agrega al prompt del sistema. Va en el
// prefijo estable, así que se resuelve una vez al arrancar y no cambia durante
// la sesión: mutarlo a media sesión tiraría la caché del proveedor.
func (c CodeSearchConfig) PromptGuidance() string {
	switch c.Prompt {
	case PromptModeEncourage:
		return "When you need to find code you cannot name exactly, prefer code_search over grep: it matches on meaning across the whole workspace."
	case PromptModeMandatory:
		return "Before editing a file, call code_search first and ask for every symbol involved in the change — the class, the method, the caller, the type — in a single call. Do not edit code you have not retrieved. Use grep only when you already know the exact string."
	default:
		return ""
	}
}
