package tips

type Tip struct {
	ID               string
	Content          string
	CooldownSessions int
}

type Context struct {
	Settings map[string]any
}
