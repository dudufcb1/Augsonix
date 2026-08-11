package builtin

import "reasonix/internal/tool"

// WriteNotifier recibe la ruta de cada archivo que una herramienta acaba de
// modificar. Existe para que el índice semántico se entere al instante de lo
// que escribe el agente, en vez de esperar a que el sondeo lo note: en una
// sesión de trabajo casi todo el cambio viene de aquí.
type WriteNotifier func(path string)

// notify avisa si hay quien escuche. Se llama solo tras una escritura correcta:
// avisar de un intento fallido haría reindexar un archivo que no cambió.
func (n WriteNotifier) notify(path string) {
	if n != nil && path != "" {
		n(path)
	}
}

// writerToolsNotify comprueba en compilación que todas las rutas que construyen
// herramientas de escritura llevan el aviso. Sin esto una ruta secundaria puede
// quedarse sin él y la función muere en silencio justo ahí.
var writerToolsNotify = []func(WriteNotifier) []tool.Tool{
	func(n WriteNotifier) []tool.Tool {
		return ConfineWriters(nil, SessionDataGuard{}, ManagedConfigPaths{}, n)
	},
	func(n WriteNotifier) []tool.Tool { return Workspace{Search: SearchSpec{OnWrite: n}}.Tools() },
}
