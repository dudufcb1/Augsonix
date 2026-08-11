package cli

// themedCommands son los subcomandos que solo aplican el tema y delegan. Viven
// en una tabla y no en el switch porque eran la misma forma repetida diez
// veces, y cada repetición costaba una rama de complejidad en la función de
// entrada. Agregar uno nuevo es una línea aquí.
var themedCommands = map[string]func([]string) int{
	"codesearch": codeSearchCommand,
	"config":     configCommand,
	"hook":       hookCommand,
	"hooks":      hookCommand,
	"mcp":        mcpCommand,
	"plugin":     pluginCommand,
	"report":     reportCommand,
	"review":     reviewCommand,
	"session":    sessionCommand,
	"task":       taskCommand,
}
