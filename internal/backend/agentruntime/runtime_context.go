package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

const (
	temporalContextMarker       = "<temporal-context>"
	localeContextMarker         = "<locale-context>"
	memoryContextMarker         = "<memory>"
	episodicMemoryContextMarker = "<episodic-memory>"
)

var transientRuntimeContextMarkers = []string{
	systemPromptSnapshotMarker,
	temporalContextMarker,
	localeContextMarker,
	browserMemoryContextMarker,
	memoryContextMarker,
	episodicMemoryContextMarker,
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (r *Runtime) SetClock(clock Clock) {
	if r == nil {
		return
	}
	if clock == nil {
		clock = systemClock{}
	}
	r.clock = clock
}

func (r *Runtime) injectSessionRuntimeContexts(ctx context.Context, userID string, session *state.Session) error {
	ensureConsumerSecurityContext(session)
	if err := r.injectPersonalization(ctx, userID, session); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) injectTransientRuntimeContexts(session *state.Session) {
	r.injectTemporalContext(session)
	r.injectLocaleContext(session)
}

func stripTransientRuntimeContexts(session *state.Session) bool {
	if session == nil || len(session.Messages) == 0 {
		return false
	}
	out := session.Messages[:0]
	changed := false
	for _, message := range session.Messages {
		if isTransientRuntimeContextMessage(message) {
			changed = true
			continue
		}
		out = append(out, message)
	}
	if changed {
		session.Messages = out
	}
	return changed
}

func isTransientRuntimeContextMessage(message state.Message) bool {
	if !message.Hidden {
		return false
	}
	for _, marker := range transientRuntimeContextMarkers {
		if marker != "" && strings.Contains(message.Content, marker) {
			return true
		}
	}
	return false
}

func (r *Runtime) baseLiveRuntimeContextParts() []string {
	parts := []string{consumerSecuritySystemContext}
	if temporalContext := r.temporalContext(); strings.TrimSpace(temporalContext) != "" {
		parts = append(parts, temporalContext)
	}
	if localeContext := r.localeContext(); strings.TrimSpace(localeContext) != "" {
		parts = append(parts, localeContext)
	}
	return parts
}

func (r *Runtime) temporalContext() string {
	if r == nil {
		return ""
	}
	now := r.now().In(r.temporalLocation())
	return formatTemporalContext(now, now.Location().String())
}

func (r *Runtime) injectTemporalContext(session *state.Session) {
	if session == nil {
		return
	}
	content := strings.TrimSpace(r.temporalContext())
	if content == "" {
		return
	}
	session.AddSystemContext(content)
}

func (r *Runtime) now() time.Time {
	if r == nil || r.clock == nil {
		return time.Now()
	}
	return r.clock.Now()
}

func (r *Runtime) temporalLocation() *time.Location {
	if r != nil {
		if name := strings.TrimSpace(r.config.Timezone); name != "" {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

func formatTemporalContext(now time.Time, timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = now.Location().String()
	}
	return fmt.Sprintf(PromptTemporalContextTemplate,
		now.Format(time.RFC3339),
		now.Format("2006-01-02"),
		now.Weekday().String(),
		timezone,
		now.Unix(),
	)
}

func (r *Runtime) localeContext() string {
	if r == nil {
		return ""
	}
	locale := strings.TrimSpace(r.config.Locale)
	if locale == "" {
		locale = "auto"
	}
	timezone := r.temporalLocation().String()
	return fmt.Sprintf(PromptLocaleContextTemplate, locale, timezone)
}

func (r *Runtime) injectLocaleContext(session *state.Session) {
	if session == nil {
		return
	}
	content := strings.TrimSpace(r.localeContext())
	if content == "" {
		return
	}
	session.AddSystemContext(content)
}
