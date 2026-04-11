package tips

type Registry struct {
	tips []Tip
}

func NewRegistry() *Registry {
	return &Registry{
		tips: []Tip{
			{ID: "memory-command", Content: "Use /memory to view and manage Claude memory", CooldownSessions: 15},
			{ID: "theme-command", Content: "Use /theme to change the color theme", CooldownSessions: 20},
			{ID: "status-line", Content: "Use /statusline to set up a custom status line", CooldownSessions: 25},
		},
	}
}

func (r *Registry) All() []Tip {
	out := make([]Tip, len(r.tips))
	copy(out, r.tips)
	return out
}

func (r *Registry) Relevant(history *History, _ *Context) []Tip {
	if history == nil {
		return r.All()
	}
	var out []Tip
	for _, tip := range r.tips {
		if history.SessionsSinceLastShown(tip.ID) >= tip.CooldownSessions {
			out = append(out, tip)
		}
	}
	return out
}
