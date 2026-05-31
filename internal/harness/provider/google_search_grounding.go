package provider

import "strings"

func googleSearchGroundingMode(request MessageRequest, config Config) string {
	mode := NormalizeGoogleSearchGroundingMode(request.GoogleSearchGrounding)
	configMode := NormalizeGoogleSearchGroundingMode(config.GoogleSearchGrounding)
	if configMode == GoogleSearchGroundingOff {
		return GoogleSearchGroundingOff
	}
	if mode != "" && mode != GoogleSearchGroundingAuto {
		return mode
	}
	if configMode != "" && configMode != GoogleSearchGroundingAuto {
		return configMode
	}
	if mode != "" {
		return mode
	}
	if configMode != "" {
		return configMode
	}
	return GoogleSearchGroundingOff
}

func shouldUseGoogleSearchGrounding(providerName, model string, mode string) bool {
	mode = NormalizeGoogleSearchGroundingMode(mode)
	if mode == "" || mode == GoogleSearchGroundingOff {
		return false
	}
	return SupportsGoogleSearchGrounding(providerName, model)
}

func SupportsGoogleSearchGrounding(providerName, model string) bool {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	switch providerName {
	case "vertex", "gcp", "gemini", "google":
	default:
		return false
	}
	model = googleSearchModelID(model)
	if model == "" {
		return false
	}
	switch {
	case strings.Contains(model, "gemini-2.0-flash"):
		return true
	case strings.Contains(model, "gemini-2.5-pro"):
		return true
	case strings.Contains(model, "gemini-2.5-flash"):
		return true
	case strings.Contains(model, "gemini-3") && strings.Contains(model, "pro"):
		return true
	case strings.Contains(model, "gemini-live-2.5-flash"):
		return true
	case strings.Contains(model, "gemini-2.5-flash-live"):
		return true
	case strings.Contains(model, "gemini-2.0-flash-live"):
		return true
	default:
		return false
	}
}

func googleSearchModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return ""
	}
	model = strings.TrimPrefix(model, "/")
	if idx := strings.LastIndex(model, "/models/"); idx >= 0 {
		return strings.TrimPrefix(model[idx+len("/models/"):], "/")
	}
	if idx := strings.LastIndex(model, "models/"); idx >= 0 {
		return strings.TrimPrefix(model[idx+len("models/"):], "/")
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	return model
}

func filterGoogleSearchFallbackTools(tools []Tool, groundingEnabled bool) []Tool {
	if !groundingEnabled || len(tools) == 0 {
		return tools
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), "WebSearch") {
			continue
		}
		out = append(out, tool)
	}
	return out
}
