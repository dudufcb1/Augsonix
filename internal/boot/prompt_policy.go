package boot

import "reasonix/internal/config"

func appendCorePolicies(prompt string) string {
	for _, policy := range []string{config.UserDecisionPolicy, config.WorkPracticePolicy, config.LanguagePolicy} {
		prompt += "\n\n" + policy
	}
	return prompt
}

func appendOfflineEnvironmentNote(prompt string, offline bool) string {
	if offline {
		prompt += "\n\n" + config.OfflineEnvironmentNote
	}
	return prompt
}

// appendCodeSearchGuidance suma la guía del modo configurado, y solo cuando la
// herramienta va a existir: instruir sobre una tool ausente manda al modelo a
// llamar algo que no está. Entra al prefijo estable, así que se resuelve una vez
// al arrancar y no cambia durante la sesión.
func appendCodeSearchGuidance(prompt string, cfg config.CodeSearchConfig, root string) string {
	if !codeSearchAvailable(cfg, root) {
		return prompt
	}
	if g := cfg.Normalized().PromptGuidance(); g != "" {
		prompt += "\n\n" + g
	}
	return prompt
}
