package settings

import (
	"os"
	"sync"
	"time"
)

type ChangeDetector struct {
	workingDir  string
	interval    time.Duration
	mdmInterval time.Duration
	sources     []SettingSource

	mu          sync.Mutex
	subscribers []chan ChangeEvent
	stopCh      chan struct{}
	stopped     bool
	snapshots   map[string]SourceSnapshot
	lastMDMPoll time.Time
}

func NewChangeDetector(workingDir string, interval time.Duration) *ChangeDetector {
	if interval <= 0 {
		interval = time.Second
	}
	return &ChangeDetector{
		workingDir:  workingDir,
		interval:    interval,
		mdmInterval: 30 * time.Minute,
		sources:     SettingSources,
		stopCh:      make(chan struct{}),
		snapshots:   make(map[string]SourceSnapshot),
	}
}

func (d *ChangeDetector) Subscribe() <-chan ChangeEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan ChangeEvent, 8)
	d.subscribers = append(d.subscribers, ch)
	return ch
}

func (d *ChangeDetector) Start() {
	go d.loop()
}

func (d *ChangeDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.stopped = true
	close(d.stopCh)
	for _, sub := range d.subscribers {
		close(sub)
	}
}

func (d *ChangeDetector) loop() {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.scan()
	for {
		select {
		case <-ticker.C:
			d.scan()
		case <-d.stopCh:
			return
		}
	}
}

func (d *ChangeDetector) scan() {
	for _, source := range d.sources {
		path := SettingsFilePathForSource(source, d.workingDir)
		if path == "" && source != SourcePolicy {
			continue
		}
		snapshot := d.snapshot(source, path)
		prev, ok := d.snapshots[path]
		d.snapshots[path] = snapshot
		if !ok {
			continue
		}
		if !prev.Exists && snapshot.Exists {
			d.broadcast(ChangeEvent{Source: source, Path: path, Type: "add"})
		} else if prev.Exists && !snapshot.Exists {
			d.broadcast(ChangeEvent{Source: source, Path: path, Type: "unlink"})
		} else if prev.Exists && snapshot.Exists && (prev.ModTime != snapshot.ModTime || prev.Size != snapshot.Size) {
			d.broadcast(ChangeEvent{Source: source, Path: path, Type: "change"})
		}
	}

	if time.Since(d.lastMDMPoll) >= d.mdmInterval {
		mdm, hkcu := RefreshMDMSettings()
		SetMDMSettingsCache(mdm, hkcu)
		d.lastMDMPoll = time.Now()
	}
}

func (d *ChangeDetector) snapshot(source SettingSource, path string) SourceSnapshot {
	info, err := os.Stat(path)
	if err != nil {
		return SourceSnapshot{Source: source, Path: path}
	}
	return SourceSnapshot{
		Source:  source,
		Path:    path,
		Exists:  true,
		ModTime: info.ModTime().UnixNano(),
		Size:    info.Size(),
	}
}

func (d *ChangeDetector) broadcast(event ChangeEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, sub := range d.subscribers {
		select {
		case sub <- event:
		default:
		}
	}
}
