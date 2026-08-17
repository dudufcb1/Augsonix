package skill

import (
	"fmt"
	"strings"

	"reasonix/internal/textutil"
)

// IndexMaxChars caps the pinned skills-index block so it can't bloat the
// cache-stable system-prompt prefix; bodies never enter the prefix.
const IndexMaxChars = 4000

// indexHeader introduces the skills block in the system prompt: the invocation
// policy (mandatory for inline, judgment-based for subagent) and how to call one.
const indexHeader = "# Skills — playbooks you can invoke\n\n" +
	"Listado de nombres, sin descripciones para no llenar el contexto: el sistema ya sugiere cada turno los skills relevantes a tu mensaje. Para leer la descripción completa o el cuerpo de un skill inline usa `read_skill({ name: \"<name>\" })`; para ejecutar usa `run_skill({ name: \"<name>\", arguments: \"<task>\" })`. En un inline, si es plausiblemente relevante a la tarea, invócalo antes de seguir en vez de prejuzgar — cargar uno imperfecto es barato. Los marcados `[🧬 subagent]` son la vía pesada: corre un subagente aislado cuyo razonamiento no entra a tu contexto; úsalos solo con relevancia fuerte, no débil. `name` es SOLO el identificador (p.ej. `\"explore\"`), NO el tag `[🧬 subagent]`. Prefiere la tool dedicada de nivel superior cuando exista para un subagente built-in. El usuario también puede invocar un skill con `/<name>`."

const readOnlyIndexHeader = "# Skills — read-only playbooks you can invoke\n\n" +
	"Listado de nombres, sin descripciones: el sistema sugiere cada turno los skills relevantes. Para leer la descripción completa o el cuerpo usa `read_skill({ name: \"<name>\" })`; para ejecutar un skill sin efectos de escritura usa `read_only_skill({ name: \"<name>\", arguments: \"<task>\" })`. En un inline, si es plausiblemente relevante, invócalo antes de seguir; cargar uno imperfecto es barato. Los `[🧬 subagent]` corren en un subagente efímero solo-lectura (solo research y bash de solo lectura; sin escrituras, instaladores, ni mutación de memoria) y se usan con relevancia fuerte. `name` es SOLO el identificador, NO el tag."

// IndexBlock renders the system/tool-result skills listing without attaching it
// to a base prompt. Only names + descriptions (+ a subagent tag) are listed;
// bodies load on demand via run_skill.
func IndexBlock(skills []Skill) string {
	return indexBlockWithHeader(indexHeader, skills)
}

// ReadOnlyIndexBlock renders the same listing with read_only_skill-specific
// invocation guidance for token-economy plan-mode connections.
func ReadOnlyIndexBlock(skills []Skill) string {
	return indexBlockWithHeader(readOnlyIndexHeader, skills)
}

func indexBlockWithHeader(header string, skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	lines := make([]string, 0, len(skills))
	for _, sk := range skills {
		// Manual-invocation skills (e.g. user-authored subagent profiles) stay
		// invocable by name (/<name>, run_skill) but must never enter the
		// pinned index the model scans for candidates to call on its own
		// initiative.
		if sk.Invocation == "manual" {
			continue
		}
		lines = append(lines, indexLine(sk))
	}
	if len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	if r := []rune(joined); len(r) > IndexMaxChars {
		joined = string(r[:IndexMaxChars]) + fmt.Sprintf("\n… (truncated %d chars)", len(r)-IndexMaxChars)
	}
	return header + "\n\n```\n" + joined + "\n```"
}

// ApplyIndex appends the skills index to basePrompt, or returns it unchanged
// when there are no skills. Only names (+ a subagent tag) are listed; bodies
// load on demand via read_skill / run_skill.
func ApplyIndex(basePrompt string, skills []Skill) string {
	block := IndexBlock(skills)
	if block == "" {
		return basePrompt
	}
	return basePrompt + "\n\n" + block
}

// indexLine renders one skill as "- name [tag]": name-only keeps dozens of user
// skills under IndexMaxChars (descriptions had been clipping the tail off the
// pinned index); bodies load on demand via read_skill / run_skill and the
// per-turn capability route surfaces relevant skills.
func indexLine(sk Skill) string {
	tag := ""
	if sk.RunAs == RunSubagent {
		tag = " [🧬 subagent]"
	}
	return "- " + sk.Name + tag
}

// clipRunes preserves the historical name but clips by grapheme clusters so
// combined emoji and other user-visible characters stay intact.
func clipRunes(s string, max int) string {
	return textutil.ClipGraphemes(s, max, "…")
}
