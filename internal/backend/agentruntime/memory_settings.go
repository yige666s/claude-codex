package agentruntime

import "time"

func defaultMemorySettings() MemorySettings {
	return MemorySettings{
		Enabled:        true,
		CaptureEnabled: true,
		ContextEnabled: true,
		UpdatedAt:      time.Now().UTC(),
	}
}

func normalizeMemorySettings(settings MemorySettings) MemorySettings {
	settings.Enabled = settings.CaptureEnabled || settings.ContextEnabled
	if settings.UpdatedAt.IsZero() {
		settings.UpdatedAt = time.Now().UTC()
	}
	return settings
}
