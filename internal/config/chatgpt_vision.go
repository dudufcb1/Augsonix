package config

func normalizeLegacyChatGPTCodexVision(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		if !p.UsesChatGPTSession() || p.Vision || p.VisionModels != nil {
			continue
		}
		p.Vision = true
		changed = true
	}
	return changed
}
