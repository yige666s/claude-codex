package agentsummary

import (
	"sync"
	"time"
)

type SummaryProvider func(previous string) (string, error)
type SummarySink func(summary string)

type Service struct {
	interval time.Duration
	mu       sync.Mutex
	stopCh   chan struct{}
}

func NewService(interval time.Duration) *Service {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Service{interval: interval}
}

func (s *Service) Start(provider SummaryProvider, sink SummarySink) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCh != nil {
		close(s.stopCh)
	}
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	go func() {
		var previous string
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				summary, err := provider(previous)
				if err == nil && summary != "" && summary != previous {
					previous = summary
					if sink != nil {
						sink(summary)
					}
				}
			case <-stopCh:
				return
			}
		}
	}()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopCh == stopCh {
			close(stopCh)
			s.stopCh = nil
		}
	}
}
