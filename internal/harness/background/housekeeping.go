package background

import "time"

type Timer interface {
	Stop() bool
}

type Tasks struct {
	RunDelayed   func()
	RunRecurring func()
}

type Options struct {
	StartupDelay       time.Duration
	RecurringInterval  time.Duration
	RecentWindow       time.Duration
	Now                func() time.Time
	GetLastInteraction func() time.Time
	AfterFunc          func(time.Duration, func()) Timer
}

type Scheduler struct {
	options Options
}

func NewScheduler(options Options) *Scheduler {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.AfterFunc == nil {
		options.AfterFunc = func(d time.Duration, fn func()) Timer {
			return time.AfterFunc(d, fn)
		}
	}
	return &Scheduler{options: options}
}

func (s *Scheduler) Start(tasks Tasks) func() {
	var timers []Timer

	if tasks.RunDelayed != nil {
		var scheduleDelayed func()
		scheduleDelayed = func() {
			if s.shouldDelay() {
				timers = append(timers, s.options.AfterFunc(s.options.StartupDelay, scheduleDelayed))
				return
			}
			tasks.RunDelayed()
		}
		timers = append(timers, s.options.AfterFunc(s.options.StartupDelay, scheduleDelayed))
	}

	if tasks.RunRecurring != nil && s.options.RecurringInterval > 0 {
		var recurring func()
		recurring = func() {
			tasks.RunRecurring()
			timers = append(timers, s.options.AfterFunc(s.options.RecurringInterval, recurring))
		}
		timers = append(timers, s.options.AfterFunc(s.options.RecurringInterval, recurring))
	}

	return func() {
		for _, timer := range timers {
			if timer != nil {
				timer.Stop()
			}
		}
	}
}

func (s *Scheduler) shouldDelay() bool {
	if s.options.GetLastInteraction == nil || s.options.RecentWindow <= 0 {
		return false
	}
	last := s.options.GetLastInteraction()
	if last.IsZero() {
		return false
	}
	return s.options.Now().Sub(last) < s.options.RecentWindow
}
