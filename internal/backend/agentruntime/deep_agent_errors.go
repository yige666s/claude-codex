package agentruntime

import (
	"fmt"
	"strings"
)

func classifyDeepAgentError(err error, result DeepAgentActionResult) string {
	var parts []string
	if err != nil {
		parts = append(parts, fmt.Sprint(err))
	}
	if strings.TrimSpace(result.Error) != "" {
		parts = append(parts, result.Error)
	}
	if errorKind := deepAgentWorkflowString(result.Metadata, "error_kind"); errorKind != "" {
		parts = append(parts, errorKind)
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join(parts, "\n")))
	if text == "" {
		return ""
	}
	switch {
	case deepAgentContainsAny(text,
		"permission denied", "operation not permitted", "eacces", "unauthorized", "forbidden",
		"docker socket", "cannot connect to the docker daemon", "/var/run/docker.sock",
		"权限", "无权限", "拒绝访问",
	):
		return DeepAgentErrorPermission
	case deepAgentContainsAny(text,
		"skill not found", "unknown skill", "tool disabled", "unknown tool", "tool not found",
		"not configured", "missing api key", "artifact service is not configured", "session store is not configured",
		"技能未找到", "工具未找到", "未配置",
	):
		return DeepAgentErrorConfig
	case deepAgentContainsAny(text,
		"empty response", "no assistant text or tool calls",
	):
		return DeepAgentErrorTransient
	case deepAgentContainsAny(text,
		"produced no artifact", "produced no artifact or report content", "validation", "invalid", "missing artifact", "artifact count 0 below required",
	):
		return DeepAgentErrorValidation
	case deepAgentContainsAny(text,
		"rate limit", "429", "deadline exceeded", "timeout", "temporarily unavailable", "connection reset",
		"network", "i/o timeout", "eof", "too many requests", "try again",
	):
		return DeepAgentErrorTransient
	case deepAgentContainsAny(text,
		"provider", "upstream", "model overloaded", "quota exceeded", "safety blocked",
	):
		return DeepAgentErrorProvider
	default:
		return DeepAgentErrorDeterministic
	}
}

func deepAgentErrorRetryable(class string) bool {
	switch strings.TrimSpace(class) {
	case DeepAgentErrorTransient, DeepAgentErrorProvider:
		return true
	default:
		return false
	}
}
