# Eduardo Custom Patches — reasonix

Parches locales sobre reasonix (`~/Dev/DeepSeek-Reasonix`) que se aplican con
`./apply-fix.sh`. Los archivos `patches/*.patch`, `apply-fix.sh` y `HANDOFF.md`
estan en `.gitignore`: no se commitean al upstream.

## 1. Image fallback para modelos sin vision (`01-image-fallback.patch`)

**Archivos:** `internal/provider/media_fallback.go` (nuevo), `internal/provider/openai.go`,
`internal/provider/anthropic.go` + tests

**Que hace:** Cuando un modelo no soporta vision (p.ej. deepseek) y le llega una
imagen como data URL, en vez de descartarla silenciosamente la guarda a
`.reasonix/attachments/read-image-*.{ext}` (fallback a `/tmp`) y reemplaza el
contenido con: `[imagen guardada en <path>. Usa el CLI read_image <path> para
verla.]`.

**Nota:** en el flujo normal de pegado, las imagenes viajan como `@ref` textual
(no como data URLs) porque `imageInputEnabled()` devuelve false para modelos sin
vision — ese caso lo cubre el patch 02. Este patch es el safety net cuando la
imagen SI llega como data URL.

## 2. Hint read_image en refs (`02-read-image-hint.patch`)

**Archivos:** `internal/control/refs.go`, `internal/control/refs_test.go`

**Que hace:** Cuando el modelo no tiene vision y se adjunta una imagen por
`@ref` (el flujo normal de pegado en la TUI), el texto que recibe el modelo en
vez del generico "use an available OCR/image/vision tool" ahora dice
explicitamente: `[imagen adjunta en @<path>; este modelo no soporta vision. Usa
el CLI read_image <path> para describirla...]`.

**Sin este patch**, el modelo intenta `Read` sobre el binario y falla con
"NUL byte detected".

## 3. Header x-opencode-session (`03-x-opencode-session.patch`)

**Archivos:** `internal/provider/provider.go`, `internal/provider/openai/host.go`,
`internal/provider/openai/openai.go`, `internal/provider/openai/session_header.go`
(nuevo), `internal/provider/anthropic/anthropic.go`, `internal/agent/gateway_session.go`
(nuevo), `internal/agent/sampling_request.go`, `internal/agent/compact.go`,
`internal/agent/coordinator.go` + tests

**Que hace:** Cuando se usa la membresia de opencode via el gateway
`opencode.ai` (providers `zen`/`opencode`), cada request LLM ahora manda el
header `x-opencode-session` con un ID estable por sesion, para que el dashboard
de opencode (`app.opencode.ai`, columna SESION) muestre algo identificable en
vez de los ultimos 8 chars aleatorios del ID nativo de opencode.

**Formato del ID:** `rx-<MMDDHHMM>-<slug>` (ej. `rx-08090022-dee-rea`, max 30
chars). El gateway persiste el header truncado a 30 chars y el dashboard lista
los ultimos 8, asi que el slug del proyecto va al final:
- `slug` = primeras 3 letras de hasta 3 palabras del workspace root
  (`DeepSeek-Reasonix` -> `dee-rea`).
- `MMDDHHMM` se extrae del session file (`NewSessionPath`), estable por sesion
  (requisito del sticky routing del gateway: mismo ID = mismo provider upstream).

**Detalle del contrato** (verificado en el repo de opencode,
`packages/console/app/src/routes/zen/util/handler.ts`): el gateway lee
`x-opencode-session`, lo usa como sticky key, y persiste
`sessionId.substring(0, 30)`; el dashboard hace `sessionID.slice(-8)`. El
header NO se filtra al proveedor real (el gateway lo borra al reenviar).

**Nota repolint:** el repo ya fallaba `go run ./tools/repolint` antes de este
patch (los parches 01/02 empujan `internal/control/refs.go` y
`internal/provider/media_fallback.go` sobre su budget). Este parche suma 1
linea a 4 archivos con deuda existente; la logica nueva vive en archivos sin
deuda. No correr `-update` a menos que se quiera absorber toda la deuda local.

## 4. UI del servidor web (`04-ui-tweaks.patch`)

**Archivos:** `internal/serve/index.html` (unico archivo; HTML embebido que
sirve `reasonix serve`). No lo revisa repolint, no toca CI.

**Que hace** (todo es UI del frontend HTTP/SSE, sin tocar el controller):

1. **Tema claro**: variables CSS light que siguen `prefers-color-scheme` del
   sistema + boton sol/luna en la sidebar (`#btn-theme`) que fuerza el tema y
   lo persiste en `localStorage['reasonix-theme']`. Script anti-flash en
   `<head>` aplica el tema guardado antes del primer paint.
2. **Markdown renderizado**: se carga `marked@12` desde CDN jsdelivr (script
   antes del script principal). Mensajes del agente, del usuario, resultados
   de tools y el historial se renderizan con markdown (negritas, listas,
   headers, code inline/bloques, blockquotes, tablas). El HTML crudo del
   modelo se DESCARTA (renderer custom) — sin XSS. Sin CDN (sin internet)
   hay fallback a texto escapado, la UI no se rompe.
3. **Preferencias de visibilidad** (persistidas en `localStorage['reasonix-prefs']`):
   switches "Expand tool results" / "Show thinking" en la sidebar. Por defecto
   las tool cards y la cadena de pensamiento se muestran EXPANDIDAS.
4. **Distintivo del turno de usuario**: `.msg--user` ahora tiene fondo
   `accent-soft`, borde izquierdo accent y padding — se distingue claramente
   del agente y de las tool calls.

**Dependencia externa:** CDN `marked@12.0.2` (jsdelivr). Si Eduardo prefiere
offline, descargar `marked.min.js` y servirlo como asset local.

**Instrucciones para reaplicar tras actualizar Reasonix (agente):**

```bash
# 1. Actualizar upstream (descarta working tree local)
cd ~/Dev/DeepSeek-Reasonix
git fetch origin
git reset --hard origin/main-v2

# 2. Verificar que los 4 patches aplican limpio
./apply-fix.sh --check

# 3. Aplicar + compilar + instalar
./apply-fix.sh --install
```

Si `--check` falla SOLO en `04-ui-tweaks.patch` (los 01-03 aplican):

```bash
# Merge 3-way contra el base del parche (recomendado primero):
git apply --3way patches/04-ui-tweaks.patch
# Si aun asi falla, rehacer a mano los tweaks (anclajes abajo)
```

**Anclajes fragiles del patch 04** (si upstream cambia estas zonas, rehacer a
mano; el patch es de UI, no logica de negocio):

- **Bloque CSS `:root`** (variables de color): los tweaks anaden un bloque
  light (`@media (prefers-color-scheme: light)` + `:root[data-theme=...]`).
  Si upstream anade/quita variables, replicar en ambos temas.
- **Funciones JS de mensajes**: `appendText`, `appendReasoning`,
  `finalizeMsg`, `addUserMsg` (markdown + buffer de streaming + pref de
  visibilidad), `renderToolDispatch`/`renderToolResult` (cards abiertas por
  defecto + markdown en el body). Si upstream las renombra o reestructura,
  portar el comportamiento: (a) assistant: acumular raw + re-render
  `renderMarkdown(raw)` en cada chunk; (b) reasoning: abrir/cerrar segun
  `PREFS.expandReasoning`; (c) user: `innerHTML=renderMarkdown(text)`.
- **i18n `__T` en/zh**: claves `theme`, `display`, `expand_tools`,
  `expand_reasoning`.
- **IMPORTANTE (TDZ)**: el bloque JS de prefs/tema/markdown usa `$` y debe
  ir DESPUES de `const $ = s => document.querySelector(s);`. Si se coloca
  antes, la ejecucion se corta en silencio y el renderer de seguridad de
  marked queda desactivado (XSS). Verificacion: en la consola del navegador
  `typeof currentRawText` debe devolver `string`; y
  `renderMarkdown('<script>x</script>')` debe descartar el tag.

**Verificacion tras reaplicar:**
- `go test ./internal/serve/` (los tests de substrings del index deben pasar).
- Visual: `reasonix serve`, enviar un mensaje con markdown (`**negritas**`,
  listas, `codigo`), confirmar: turno de usuario con fondo, cards de tools
  abiertas, reasoning visible, toggle de tema persiste tras recarga.

## Dependencias externas (no son patches)

- **CLI `read_image`**: `~/asistente/read_image/read_image.py` + symlink en
  `~/.local/bin/read_image`. Describe una imagen con gpt-4.1 via el proxy local
  (`127.0.0.1:4141`). El aviso del patch 02 le dice al modelo que lo use.
- **System prompt custom**: `~/.reasonix/prompts/session.md` apuntado con
  `system_prompt_file` en `~/.reasonix/config.toml`. Contiene el perfil de
  Eduardo (comunicacion, filosofia, scope, critical thinking, docker/traefik,
  dev env, file management, workflow, code style, type checking), la seccion de
  semantic search (`codebase`), y password sudo / transcripcion deepgram. No
  toca `DefaultSystemPrompt` (guard de 240 bytes del repo).
- **Sandbox**: `~/.reasonix/config.toml` tiene `[sandbox] bash = "off"` para que
  bash (y por tanto `read_image`) pueda leer paths fuera del workspace.
- **Providers opencode (membresia)**: en `~/.reasonix/config.toml` hay dos
  providers que conectan la membresia de opencode (key en `~/.reasonix/.env`
  como `OPENCODE_API_KEY`, sacada de `~/.local/share/opencode/auth.json`):
  - `zen` → `base_url = "https://opencode.ai/zen/v1"`, modelo
    `deepseek-v4-flash-free` (gratis, sin creditos).
  - `opencode` → `base_url = "https://opencode.ai/zen/go/v1"` (membresia Go,
    cobra usage del plan), modelos `deepseek-v4-flash` (default global) y
    `deepseek-v4-pro`. El endpoint del plan Go es `/zen/go/v1`, distinto del
    `/zen/v1` de creditos por consumo.
  - Se eliminaron los providers `deepseek` nativo (sin saldo) y `nvidia`
    (inestable).
  - Uso: `reasonix --model opencode/deepseek-v4-flash` o `zen/deepseek-v4-flash-free`.
- **MCP importados de opencode**: en `~/.reasonix/config.toml` hay 8 `[[plugins]]`
  (deepwiki, slack, ScraplingServer, n8n-god, n8n-test, automaton, wpwizard,
  kdenlive-mcp). Los secrets (slack tokens, n8n keys, wpwizard bearer) viven en
  `~/.reasonix/.env` y se referencian con `${VAR}`. Se excluyeron a proposito:
  `leer-imagen` (reasonix usa el CLI `read_image`), `perplexity-server`,
  `cassette` y `ssh-mcp` (se conecta por bash directo). Con varios MCP
  auto-start, el arranque es mas lento; `auto_start = false` en un plugin lo
  conecta bajo demanda con `/mcp`.

## Hooks portados de Claude Code

`~/.reasonix/settings.json` (global) registra hooks que reutilizan los scripts
de `~/.claude/hooks/`. Los scripts se adaptaron para leer el payload en formato
dual (Claude `tool_input`/`tool_response` y Reasonix nativo `toolArgs`/`sessionId`):

- `file_size_guard.sh` — PostToolUse en `edit_file|write_file|multi_edit`.
  **Anti monolitos**: aviso a 500 lineas, duro a 600 (obliga a partir), tablas
  declaradas 1500, markdown con aviso de progressive disclosure. No bloquea.
- `grep_semantic_reminder.sh` — PostToolUse en `grep` (count) y `bash` (reset).
  Recuerda usar `codebase -ss` cuando se acumulan 3 greps sin busqueda semantica.
- `project_sentinel.sh` — SessionStart; busca `.reasonix/on-session-start` en el
  arbol (via `REF_DIR` env, default `.claude`).
- `project_gate.sh` — Stop; busca `.reasonix/on-stop` (via `REF_DIR`).

Los hooks se listan con `reasonix hook list --json` y se gestionan en la TUI con
`/hooks`. El anti monolitos NO es por proyecto: es global (en `~/.reasonix/settings.json`).

## Skills

Reasonix descubre skills con las mismas convenciones que Claude
(`config.ConventionDirs`): `.reasonix/skills`, `.agents/skills`, `.agent/skills`,
`.claude/skills` en proyecto y home, mas `[skills] paths`. Verificado: detecta
los 85 skills de `~/.claude/skills` y `~/.agents/skills` (mismo formato SKILL.md
con frontmatter `name:`/`description:`). El modelo los invoca con `run_skill`/
`explore`, Eduardo con `/<nombre>`. Los skills que viven solo en
`~/.config/opencode/skills` NO se ven; para incluirlos:
`[skills] paths = ["~/.config/opencode/skills"]`.

## Guardian en Reasonix

`guardian init --reasonix` (o default: instala los 4 adapters) escribe
`.reasonix/settings.json` del proyecto con `PostToolUse` (edit_file|write_file|
multi_edit -> `guardian hook file`) y `Stop` -> `guardian hook stop`. El adapter
vive en `~/guardian/installers.py` (`install_reasonix` + `REASONIX_HOOKS`).
Funciona porque reasonix confia todos los proyectos y adapta el payload a
`tool_input.file_path`. Las reglas del revisor LLM (anti monolitos incluido)
viven en `~/.guardian/review.json` (agnostico de plataforma).

## Como usar

```bash
# Actualizar upstream primero (descarta cambios locales del working tree)
cd ~/Dev/DeepSeek-Reasonix
git fetch origin
git reset --hard origin/main-v2

# Aplicar parches + compilar
./apply-fix.sh

# Aplicar + compilar + instalar el binario en el paquete npm (sudo)
./apply-fix.sh --install

# Verificar que los parches aplican limpio (sin aplicar)
./apply-fix.sh --check
```

## Como crear un parche nuevo

```bash
# Edita los archivos que quieras, luego:
cd ~/Dev/DeepSeek-Reasonix
git add -N <archivos nuevos>          # marca untracked como intent-to-add
git diff -- <ruta/afectada/> > patches/NN-nombre.patch
git reset -q                          # limpia el intent-to-add (sin perder cambios)

# Verifica en un clon limpio:
git clone -q ~/Dev/DeepSeek-Reasonix /tmp/rx-test && cd /tmp/rx-test
git apply --check ../patches/NN-nombre.patch
```

## Notas

- El script aplica TODOS los `patches/*.patch` en orden (lexicografico).
- Si un parche no aplica limpio, usa fuzz (`-C2`); si ya esta aplicado, lo salta.
- `--install` respalda el binario previo como `reasonix.pre-patch` antes de
  reemplazar con `mv` atomico (funciona con reasonix abierto).
- El binario instalado vive en
  `/usr/local/lib/node_modules/reasonix/node_modules/@reasonix/cli-linux-x64/bin/reasonix`
  (paquete npm). `reasonix version` muestra "dev" para builds locales.
