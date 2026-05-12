package provider

// QwenProvider uses Alibaba Cloud Model Studio / DashScope OpenAI-compatible
// chat completions for Qwen models.
type QwenProvider struct {
	*OpenAIProvider
}

const (
	defaultQwenBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultQwenModel   = "qwen-plus"
)

func NewQwenProvider(cfg Config) (*QwenProvider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultQwenBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultQwenModel
	}
	cfg.Provider = "qwen"
	openai, err := NewOpenAIProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &QwenProvider{OpenAIProvider: openai}, nil
}

func (p *QwenProvider) Name() string {
	return "qwen"
}

func (p *QwenProvider) SupportedModels() []string {
	return []string{
		"qwen-plus",
		"qwen-max",
		"qwen-flash",
		"qwen-turbo",
		"qwen3.5-plus",
		"qwen3.5-max",
		"qwen3.5-flash",
	}
}
