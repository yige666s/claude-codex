package magicdocs

import (
	"regexp"
	"sync"
)

var (
	headerPattern  = regexp.MustCompile(`(?im)^#\s*MAGIC\s+DOC:\s*(.+)$`)
	italicsPattern = regexp.MustCompile(`^[_*](.+?)[_*]\s*$`)
)

type Info struct {
	Path         string
	Title        string
	Instructions string
}

type Service struct {
	mu   sync.Mutex
	docs map[string]Info
}

func NewService() *Service {
	return &Service{docs: map[string]Info{}}
}

func DetectHeader(content string) (*Info, bool) {
	match := headerPattern.FindStringSubmatch(content)
	if match == nil || match[1] == "" {
		return nil, false
	}
	info := &Info{Title: match[1]}
	lines := regexp.MustCompile(`\r?\n`).Split(content, -1)
	for i, line := range lines {
		if headerPattern.MatchString(line) && i+1 < len(lines) {
			if italics := italicsPattern.FindStringSubmatch(lines[i+1]); italics != nil {
				info.Instructions = italics[1]
			}
			break
		}
	}
	return info, true
}

func (s *Service) Register(path string, content string) bool {
	info, ok := DetectHeader(content)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info.Path = path
	s.docs[path] = *info
	return true
}

func (s *Service) Tracked() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, 0, len(s.docs))
	for _, info := range s.docs {
		out = append(out, info)
	}
	return out
}
