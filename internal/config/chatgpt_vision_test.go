package config

import "testing"

func TestNormalizeLegacyChatGPTCodexVision(t *testing.T) {
	legacy := chatGPTCodexPreset.Entries[0]
	legacy.Vision = false
	legacy.VisionModels = nil

	disabled := legacy
	disabled.Name = "chatgpt-codex-disabled"
	disabled.VisionModels = []string{}

	other := legacy
	other.Name = "other"
	other.BaseURL = "https://example.com/v1"
	other.RequestURL = ""

	c := &Config{Providers: []ProviderEntry{legacy, disabled, other}}
	if !normalizeLegacyChatGPTCodexVision(c) {
		t.Fatal("legacy ChatGPT Codex provider was not migrated")
	}
	if !c.Providers[0].Vision || !EffectiveVision(&c.Providers[0]) {
		t.Fatalf("migrated ChatGPT Codex provider = %+v", c.Providers[0])
	}
	if c.Providers[1].Vision || c.Providers[2].Vision {
		t.Fatalf("explicitly configured or unrelated provider was migrated: %+v", c.Providers)
	}
}
