package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
)

const defaultBetaContentLimit = 60 * 1024

type BetaOptions struct {
	Enabled        bool
	LogUserPrompts bool
	MaxContentSize int
}

type BetaSessionTracer struct {
	next           SessionTracer
	logUserPrompts bool
	maxContentSize int
	mu             sync.Mutex
	seenHashes     map[string]bool
}

func NewBetaSessionTracer(next SessionTracer, options BetaOptions) *BetaSessionTracer {
	if options.MaxContentSize <= 0 {
		options.MaxContentSize = defaultBetaContentLimit
	}
	return &BetaSessionTracer{
		next:           next,
		logUserPrompts: options.LogUserPrompts,
		maxContentSize: options.MaxContentSize,
		seenHashes:     map[string]bool{},
	}
}

func (t *BetaSessionTracer) Record(event TraceEvent) error {
	event = cloneTraceEvent(event)
	event.Attrs = t.transformAttrs(event.Attrs)
	return t.next.Record(event)
}

func (t *BetaSessionTracer) Close() error {
	return t.next.Close()
}

func (t *BetaSessionTracer) transformAttrs(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return attrs
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string]any, len(attrs))
	for key, value := range attrs {
		switch text := value.(type) {
		case string:
			normalizedKey := strings.ToLower(key)
			if normalizedKey == "prompt" && !t.logUserPrompts {
				result[key] = "<REDACTED>"
				continue
			}
			truncated, didTruncate := TruncateContent(text, t.maxContentSize)
			result[key] = truncated
			if didTruncate {
				result[key+"_truncated"] = true
				result[key+"_original_length"] = len(text)
			}
			if normalizedKey == "system_prompt" {
				hash := ShortHash(text)
				if t.seenHashes[hash] {
					result[key] = "<DEDUPED>"
					result[key+"_hash"] = hash
					continue
				}
				t.seenHashes[hash] = true
				result[key+"_hash"] = hash
			}
		default:
			result[key] = value
		}
	}
	return result
}

func IsBetaTracingEnabled() bool {
	return isTruthyEnv(os.Getenv("ENABLE_BETA_TRACING_DETAILED")) && strings.TrimSpace(os.Getenv("BETA_TRACING_ENDPOINT")) != ""
}

func TruncateContent(content string, limit int) (string, bool) {
	if limit <= 0 || len(content) <= limit {
		return content, false
	}
	if limit <= 3 {
		return content[:limit], true
	}
	return content[:limit-3] + "...", true
}

func ShortHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12]
}

func ClearBetaTracingState(tracer *BetaSessionTracer) {
	if tracer == nil {
		return
	}
	tracer.mu.Lock()
	defer tracer.mu.Unlock()
	tracer.seenHashes = map[string]bool{}
}

func isTruthyEnv(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
