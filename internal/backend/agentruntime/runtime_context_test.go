package agentruntime

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTemporalContextIncludesStableDateFields(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	now := time.Date(2026, 5, 27, 14, 30, 0, 0, location)

	got := formatTemporalContext(now, "Asia/Shanghai")

	for _, want := range []string{
		"<temporal-context>",
		"Current datetime: 2026-05-27T14:30:00+08:00",
		"Current date: 2026-05-27",
		"Current weekday: Wednesday",
		"Timezone: Asia/Shanghai",
		"Unix timestamp:",
		"</temporal-context>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("temporal context missing %q:\n%s", want, got)
		}
	}
}

func TestLocaleContextUsesConfiguredLocaleAndTimezone(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{Locale: "zh-CN", Timezone: "Asia/Shanghai"}, nil, nil, nil, nil)

	got := runtime.localeContext()

	for _, want := range []string{
		"<locale-context>",
		"Locale: zh-CN",
		"Timezone: Asia/Shanghai",
		"Language policy:",
		"</locale-context>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("locale context missing %q:\n%s", want, got)
		}
	}
}
