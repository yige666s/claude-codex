package tips

type History struct {
	NumStartups int
	TipsHistory map[string]int
}

func (h *History) RecordTipShown(tipID string) {
	if h.TipsHistory == nil {
		h.TipsHistory = map[string]int{}
	}
	h.TipsHistory[tipID] = h.NumStartups
}

func (h *History) SessionsSinceLastShown(tipID string) int {
	if h.TipsHistory == nil {
		return int(^uint(0) >> 1)
	}
	last, ok := h.TipsHistory[tipID]
	if !ok {
		return int(^uint(0) >> 1)
	}
	return h.NumStartups - last
}
