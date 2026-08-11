# Augsonix

Agente de código para terminal, con **motor de búsqueda semántica propio**.

Es un fork de [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix).
El upstream sigue siendo la base y de ahí se traen mejoras a mano, eligiendo qué
entra; este repositorio existe para que los cambios de abajo no dependan de que
alguien más los acepte.

---

## Por qué existe

Reasonix es un CLI excelente y muy barato de operar: mantiene el prefijo del
prompt estable entre turnos, así que DeepSeek lo reconoce y cobra el 2% por lo
que ya vio. Sobre esa base faltaba una pieza: **encontrar código por
significado**, no por coincidencia de texto.

`grep` sirve cuando ya sabes la palabra exacta. No sirve cuando recuerdas qué
hace algo pero no cómo se llama, cuando el código está en otro idioma que tu
pregunta, o cuando el concepto está repartido entre archivos que no comparten
ni una cadena. Eso es lo que resuelve este fork.

---

## Qué agrega sobre el upstream

### Búsqueda semántica del código (`code_search`)

Indexa el workspace en vectores y responde preguntas en lenguaje natural
devolviendo el código relevante con archivo y rango de líneas.

- **Troceo por fronteras de declaración.** El corte respeta funciones y clases
  en lugar de partir cada 5000 caracteres: `go/parser` para Go, un escáner de
  llaves para PHP, TypeScript y JavaScript, y sangría para Python. Medido sobre
  5526 archivos reales, las declaraciones partidas bajan de 19.7% a 5.4%.
- **La garantía no es que el corte sea óptimo** —es una heurística— **sino que
  nunca mienta**: el texto de un bloque son exactamente las líneas que dice
  contener, los bloques no se pisan y entre todos cubren el archivo. Una
  detección incoherente se descarta entera y se vuelve al troceo por caracteres.
- **Dos etapas**: barrido vectorial amplio y reordenamiento leyendo consulta y
  código juntos, que es lo que un embedding no puede hacer.
- **El índice se reconstruye solo** cuando cambia cómo se construyó. La receta
  —versión del troceo y modelo de embeddings— va dentro del hash de cada
  archivo, así que cambiar cualquiera de los dos invalida lo guardado y se
  reindexa sin que nadie tenga que acordarse.

### Búsqueda sobre la historia (`git_commit_search`)

Responde "cómo se hizo antes un cambio parecido" o "por qué esto quedó así".
Indexa el mensaje del commit, los archivos tocados y un recorte del diff, sin
ningún modelo generando descripciones.

Medido contra `git log --grep` sobre 210 commits reales, con preguntas escritas
con otras palabras que las del commit: **14 de 15 contra 10 de 15**, y 12 de
esas 14 en el primer resultado. Donde más gana es cuando el vocabulario no
coincide —preguntas "cobrar dos veces" y el commit dice "idempotent webhooks"—
y sobre todo cuando la historia mezcla idiomas.

### Almacén compartido y portable

- **Postgres con pgvector**, o local si no quieres infraestructura.
- La identidad del workspace se deriva del remoto de git, no de la ruta: mover
  la carpeta o abrir el proyecto en otra máquina no obliga a reindexar.
- El índice vive fuera del repositorio, así que no ensucia el árbol de trabajo.

### Varias credenciales con relevo automático

Cuando una cuenta se queda sin cuota entra la siguiente y el trabajo sigue donde
iba. La decisión es por comportamiento y no por código de error: una ráfaga
pasajera cede a los pocos reintentos, una cuenta sin margen no cede nunca.

### Indexado rápido

El escaneo procesa varios archivos a la vez y las sentencias van a la base en un
solo envío. Un proyecto de 62 archivos pasó de 58 segundos a 5.

### Fricción configurable sobre `grep`

Cuando el agente encadena búsquedas de texto sin consultar el índice, se le
puede pedir que lo intente por significado. Tres modos, del aviso al bloqueo.

### Otros parches sobre el upstream

- **Imágenes en modelos sin visión**: en vez de descartarlas en silencio, se
  guardan y se le dice al modelo cómo leerlas.
- **Cabecera `x-opencode-session`**: identifica la sesión en el panel del
  gateway con algo legible en lugar de ocho caracteres al azar.
- Ajustes de interfaz: salida de herramientas visible, forma del cursor,
  título de ventana, indicador de estado del índice.

---

## Relación con el upstream

```
origin    → este repositorio (donde se trabaja)
upstream  → esengine/DeepSeek-Reasonix (de donde se eligen mejoras)
```

Para revisar qué hay de nuevo arriba:

```bash
git fetch upstream
git log --oneline HEAD..upstream/main-v2
```

No hay seguimiento automático: los cambios del upstream se miran y se traen uno
por uno, para que nada de aquí se pierda en un merge.

---

## Documentación

La del proyecto base sigue vigente; este fork no la cambia.

| | |
|---|---|
| [Guía](docs/GUIDE.md) | Empezar a usarlo |
| [CLI](docs/CLI.md) | Comandos y banderas |
| [Rutas de configuración](docs/CONFIG_PATHS.md) | Dónde vive cada archivo |
| [Checkpoints](docs/CHECKPOINTS.md) | Deshacer cambios |
| [Recuperación](docs/RECOVERY.md) | Cuando algo sale mal |
| [Extensiones](docs/EXTENSIONS.md) · [Protocolo](docs/EXTENSION_PROTOCOL.md) · [Paquetes](docs/PLUGIN_PACKAGES.md) | Ampliarlo |
| [ACP](docs/ACP.md) | Integración con editores |
| [Bot](docs/BOT_GUIDE.md) | Modo bot |
| [Diagnóstico de capacidades](docs/CAPABILITY_DIAGNOSTICS.md) | Qué soporta cada modelo |
| [Migración](docs/MIGRATING.md) | Venir de otra versión |
| [README original](README.zh-CN.md) | El del upstream, en chino |

---

## Compilar

```bash
make build          # binario en bin/
go test ./...
go run ./tools/repolint
```

Sin CGO, un solo binario estático, con compilación cruzada a las plataformas del
upstream.

---

## Créditos

Todo el mérito de la base es de [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)
y de su autor. Este fork solo agrega lo de arriba.
