package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformWindows Platform = "windows"
	PlatformWSL     Platform = "wsl"
	PlatformLinux   Platform = "linux"
	PlatformUnknown Platform = "unknown"
)

var (
	platformOnce sync.Once
	platformVal  Platform
	wslOnce      sync.Once
	wslVersion   string
)

func GetPlatform() Platform {
	platformOnce.Do(func() {
		switch runtime.GOOS {
		case "darwin":
			platformVal = PlatformMacOS
		case "windows":
			platformVal = PlatformWindows
		case "linux":
			data, err := os.ReadFile("/proc/version")
			if err == nil {
				lower := strings.ToLower(string(data))
				if strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl") {
					platformVal = PlatformWSL
					return
				}
			}
			platformVal = PlatformLinux
		default:
			platformVal = PlatformUnknown
		}
	})
	return platformVal
}

func GetWslVersion() string {
	wslOnce.Do(func() {
		if runtime.GOOS != "linux" {
			return
		}
		data, err := os.ReadFile("/proc/version")
		if err != nil {
			return
		}
		lower := strings.ToLower(string(data))
		switch {
		case strings.Contains(lower, "wsl2"):
			wslVersion = "2"
		case strings.Contains(lower, "wsl"):
			wslVersion = "1"
		case strings.Contains(lower, "microsoft"):
			wslVersion = "1"
		}
	})
	return wslVersion
}

func DetectVCS(dir string) ([]string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	detected := make([]string, 0, 4)
	seen := map[string]bool{}
	markers := [][2]string{
		{".git", "git"},
		{".hg", "mercurial"},
		{".svn", "svn"},
		{".jj", "jujutsu"},
		{".sl", "sapling"},
		{".tfvc", "tfs"},
		{"$tf", "tfs"},
	}
	for _, entry := range entries {
		name := entry.Name()
		for _, marker := range markers {
			if name == marker[0] && !seen[marker[1]] {
				seen[marker[1]] = true
				detected = append(detected, marker[1])
			}
		}
	}
	if os.Getenv("P4PORT") != "" && !seen["perforce"] {
		detected = append(detected, "perforce")
	}
	return detected, nil
}

func NormalizeComparablePath(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
