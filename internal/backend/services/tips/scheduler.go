package tips

func SelectTipWithLongestTimeSinceShown(available []Tip, history *History) *Tip {
	if len(available) == 0 {
		return nil
	}
	selected := available[0]
	best := history.SessionsSinceLastShown(selected.ID)
	for _, tip := range available[1:] {
		if sessions := history.SessionsSinceLastShown(tip.ID); sessions > best {
			selected = tip
			best = sessions
		}
	}
	return &selected
}

func GetTipToShow(history *History, registry *Registry, ctx *Context, spinnerTipsEnabled bool) *Tip {
	if !spinnerTipsEnabled || registry == nil || history == nil {
		return nil
	}
	return SelectTipWithLongestTimeSinceShown(registry.Relevant(history, ctx), history)
}
