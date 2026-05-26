package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/googleauth"
	"claude-codex/internal/harness/anthropic"
	"claude-codex/internal/harness/engine"
	providerbackend "claude-codex/internal/harness/provider"
)

type LLMConfig struct {
	Provider       string
	Model          string
	APIKey         string
	Token          string
	BaseURL        string
	Timeout        int
	VertexLocation string
}

func BuildLLMConfig(providerName, model, apiKey, apiToken, apiBaseURL string, timeout int) (LLMConfig, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if providerName == "" {
		providerName = "anthropic"
	}
	defaults, err := providerbackend.NewFactory().DefaultConfig(providerName)
	if err != nil {
		return LLMConfig{}, err
	}
	cfg := LLMConfig{
		Provider: defaults.Provider,
		Model:    startupconfig.FirstNonEmpty(model, defaults.Model),
		BaseURL:  startupconfig.FirstNonEmpty(apiBaseURL, providerEnvBaseURL(providerName), defaults.BaseURL),
		APIKey:   startupconfig.FirstNonEmpty(apiKey, providerEnvAPIKey(providerName)),
		Token:    startupconfig.FirstNonEmpty(apiToken, providerEnvToken(providerName)),
		Timeout:  timeout,
	}
	if strings.EqualFold(cfg.Provider, "vertex") || strings.EqualFold(cfg.Provider, "gcp") {
		cfg.VertexLocation = startupconfig.FirstNonEmpty(os.Getenv("VERTEX_LOCATION"), os.Getenv("GOOGLE_CLOUD_LOCATION"), os.Getenv("CLOUD_ML_REGION"), "us-central1")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if requiresCredential(cfg.Provider) && cfg.APIKey == "" && cfg.Token == "" && !providerHasAmbientCredential(cfg.Provider) {
		return LLMConfig{}, fmt.Errorf("credential required for llm provider %q", cfg.Provider)
	}
	if isCustomProvider(providerName) && strings.TrimSpace(cfg.BaseURL) == "" {
		return LLMConfig{}, fmt.Errorf("custom provider requires -api-base-url or AGENT_API_LLM_BASE_URL")
	}
	return cfg, nil
}

func newPlanner(cfg LLMConfig) (engine.Planner, error) {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic", "claude":
		credential := startupconfig.FirstNonEmpty(cfg.APIKey, cfg.Token)
		client := anthropic.NewClient(credential, cfg.BaseURL, time.Duration(cfg.Timeout)*time.Second)
		return anthropic.NewPlanner(client, cfg.Model), nil
	case "custom", "openai-compatible", "baseurl":
		provider, err := providerbackend.NewOpenAIProvider(providerbackend.Config{
			Provider: "openai",
			APIKey:   startupconfig.FirstNonEmpty(cfg.APIKey, cfg.Token),
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			Timeout:  cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return providerbackend.NewPlanner(provider, cfg.Model), nil
	default:
		provider, err := providerbackend.NewFactory().CreateProvider(providerbackend.Config{
			Provider:       cfg.Provider,
			APIKey:         cfg.APIKey,
			Token:          cfg.Token,
			BaseURL:        cfg.BaseURL,
			Model:          cfg.Model,
			Timeout:        cfg.Timeout,
			VertexLocation: cfg.VertexLocation,
		})
		if err != nil {
			return nil, err
		}
		return providerbackend.NewPlanner(provider, cfg.Model), nil
	}
}

func NewGovernedPlannerForScope(primary LLMConfig, fallbackSpec, modelRoutes string, scope agentruntime.Scope, usageStore agentruntime.LLMUsageStore, governance agentruntime.LLMGovernanceConfig) (*agentruntime.GovernedPlanner, error) {
	primary.Model = RoutedModel(primary.Model, modelRoutes, scope)
	configs := []LLMConfig{primary}
	for _, fallback := range ParseLLMFallbacks(fallbackSpec, primary.Timeout) {
		if fallback.Model == "" {
			fallback.Model = primary.Model
		}
		if fallback.VertexLocation == "" {
			fallback.VertexLocation = primary.VertexLocation
		}
		configs = append(configs, fallback)
	}
	backends := make([]agentruntime.LLMBackend, 0, len(configs))
	for i, cfg := range configs {
		planner, err := newPlanner(cfg)
		if err != nil {
			return nil, err
		}
		name := cfg.Provider
		if i > 0 {
			name = fmt.Sprintf("%s-fallback-%d", cfg.Provider, i)
		}
		backends = append(backends, agentruntime.LLMBackend{
			Name:     name,
			Provider: cfg.Provider,
			Model:    cfg.Model,
			Planner:  planner,
		})
	}
	return agentruntime.NewGovernedPlanner(backends, usageStore, governance)
}

func ApplyRuntimeLLMConfig(base LLMConfig, runtimeConfig agentruntime.LLMGovernanceConfig) LLMConfig {
	if strings.TrimSpace(runtimeConfig.Provider) != "" {
		base.Provider = strings.TrimSpace(runtimeConfig.Provider)
	}
	if strings.TrimSpace(runtimeConfig.Model) != "" {
		base.Model = strings.TrimSpace(runtimeConfig.Model)
	}
	if strings.TrimSpace(runtimeConfig.VertexLocation) != "" {
		base.VertexLocation = strings.TrimSpace(runtimeConfig.VertexLocation)
	}
	return base
}

func ParseLLMFallbacks(value string, timeout int) []LLMConfig {
	specs := startupconfig.SplitCSV(value)
	out := make([]LLMConfig, 0, len(specs))
	for _, spec := range specs {
		parts := strings.SplitN(spec, ":", 2)
		providerName := strings.TrimSpace(parts[0])
		if providerName == "" {
			continue
		}
		model := ""
		if len(parts) == 2 {
			model = strings.TrimSpace(parts[1])
		}
		cfg, err := BuildLLMConfig(providerName, model, "", "", "", timeout)
		if err != nil {
			logInfof("warning: skipping llm fallback %q: %v", spec, err)
			continue
		}
		out = append(out, cfg)
	}
	return out
}

func RoutedModel(currentModel, routes string, scope agentruntime.Scope) string {
	routeMap := ParseModelRoutes(routes)
	if scope.SkillName != "" {
		if model := routeMap["skill:"+scope.SkillName]; model != "" {
			return model
		}
	}
	if scope.SkillScoped {
		if model := routeMap["skill"]; model != "" {
			return model
		}
	}
	if !scope.SkillScoped {
		class := chatRouteClass(scope.Prompt)
		if model := routeMap["chat:"+class]; model != "" {
			return model
		}
	}
	if model := routeMap["chat"]; model != "" && !scope.SkillScoped {
		return model
	}
	if model := routeMap["default"]; model != "" {
		return model
	}
	return currentModel
}

func chatRouteClass(prompt string) string {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if text == "" {
		return "normal"
	}
	searchMarkers := []string{"搜索", "查询", "查一下", "搜一下", "search", "websearch", "天气", "weather", "新闻", "latest", "最新"}
	for _, marker := range searchMarkers {
		if strings.Contains(text, marker) {
			return "search"
		}
	}
	complexMarkers := []string{"复杂", "深入", "详细", "完整", "分析", "报告", "方案", "架构", "设计", "文档", "docx", "ppt", "高质量", "推理", "评估", "规划", "review", "analyze", "architecture", "document", "report", "proposal"}
	for _, marker := range complexMarkers {
		if strings.Contains(text, marker) {
			return "complex"
		}
	}
	if len([]rune(text)) > 700 {
		return "complex"
	}
	return "normal"
}

func ParseModelRoutes(value string) map[string]string {
	out := make(map[string]string)
	for _, item := range startupconfig.SplitCSV(value) {
		key, model, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		model = strings.TrimSpace(model)
		if key != "" && model != "" {
			out[key] = model
		}
	}
	return out
}

func providerEnvAPIKey(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "anthropic", "claude":
		return startupconfig.FirstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("CLAUDE_API_KEY"))
	case "openai", "gpt", "custom", "openai-compatible", "baseurl":
		return startupconfig.FirstNonEmpty(os.Getenv("OPENAI_API_KEY"), os.Getenv("AGENT_API_LLM_API_KEY"))
	case "qwen", "dashscope", "aliyun":
		return startupconfig.FirstNonEmpty(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"), os.Getenv("ALIBABA_CLOUD_API_KEY"), os.Getenv("AGENT_API_LLM_API_KEY"))
	case "gemini", "google":
		return startupconfig.FirstNonEmpty(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	case "vertex", "gcp":
		return ""
	default:
		return os.Getenv("AGENT_API_LLM_API_KEY")
	}
}

func providerEnvToken(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "vertex", "gcp":
		return startupconfig.FirstNonEmpty(os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("GOOGLE_ACCESS_TOKEN"))
	default:
		return os.Getenv("AGENT_API_LLM_TOKEN")
	}
}

func providerHasAmbientCredential(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "vertex", "gcp":
		return googleauth.HasGoogleApplicationCredentialsEnv()
	default:
		return false
	}
}

func providerEnvBaseURL(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "anthropic", "claude":
		return os.Getenv("ANTHROPIC_BASE_URL")
	case "openai", "gpt":
		return os.Getenv("OPENAI_BASE_URL")
	case "qwen", "dashscope", "aliyun":
		return startupconfig.FirstNonEmpty(os.Getenv("DASHSCOPE_BASE_URL"), os.Getenv("QWEN_BASE_URL"), os.Getenv("AGENT_API_LLM_BASE_URL"))
	case "gemini", "google":
		return os.Getenv("GEMINI_BASE_URL")
	case "vertex", "gcp":
		return os.Getenv("VERTEX_BASE_URL")
	case "custom", "openai-compatible", "baseurl":
		return startupconfig.FirstNonEmpty(os.Getenv("AGENT_API_LLM_BASE_URL"), os.Getenv("OPENAI_BASE_URL"))
	default:
		return os.Getenv("AGENT_API_LLM_BASE_URL")
	}
}

func requiresCredential(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "simple":
		return false
	default:
		return true
	}
}

func isCustomProvider(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "custom", "openai-compatible", "baseurl":
		return true
	default:
		return false
	}
}

func LLMConfigReadinessCheck(cfg LLMConfig) func(context.Context) error {
	return func(context.Context) error {
		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			return fmt.Errorf("llm provider is required")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return fmt.Errorf("llm model is required")
		}
		if isCustomProvider(provider) && strings.TrimSpace(cfg.BaseURL) == "" {
			return fmt.Errorf("custom llm provider requires base URL")
		}
		if requiresCredential(provider) && strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(cfg.Token) == "" && !providerHasAmbientCredential(provider) {
			return fmt.Errorf("llm credential is required for provider %q", provider)
		}
		switch provider {
		case "vertex", "gcp":
			if strings.TrimSpace(cfg.Token) == "" && strings.TrimSpace(cfg.APIKey) == "" && !googleauth.HasGoogleApplicationCredentialsEnv() {
				return fmt.Errorf("vertex credential is required; set GOOGLE_APPLICATION_CREDENTIALS, GOOGLE_APPLICATION_CREDENTIALS_JSON, or VERTEX_ACCESS_TOKEN")
			}
			if option, ok := agentruntime.LLMModelOptionFor(strings.TrimSpace(cfg.Model)); ok && strings.TrimSpace(cfg.VertexLocation) != "" && strings.TrimSpace(cfg.VertexLocation) != option.VertexLocation {
				return fmt.Errorf("vertex location for %s must be %s", option.ID, option.VertexLocation)
			}
			if !strings.Contains(strings.TrimSpace(cfg.Model), "/") && startupconfig.FirstNonEmpty(os.Getenv("VERTEX_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCLOUD_PROJECT")) == "" {
				return fmt.Errorf("vertex project ID is required for short model names")
			}
		}
		return nil
	}
}
