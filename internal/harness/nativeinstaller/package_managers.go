package nativeinstaller

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type PackageManager string

const (
	PMHomebrew PackageManager = "homebrew"
	PMWinget   PackageManager = "winget"
	PMPacman   PackageManager = "pacman"
	PMDeb      PackageManager = "deb"
	PMRPM      PackageManager = "rpm"
	PMApk      PackageManager = "apk"
	PMMise     PackageManager = "mise"
	PMAsdf     PackageManager = "asdf"
	PMUnknown  PackageManager = "unknown"
)

type OSRelease struct {
	ID     string
	IDLike []string
}

func ParseOSRelease(content string) OSRelease {
	scanner := bufio.NewScanner(strings.NewReader(content))
	result := OSRelease{}
	for scanner.Scan() {
		line := scanner.Text()
		if value, ok := parseOSReleaseLine(line, "ID"); ok {
			result.ID = value
		}
		if value, ok := parseOSReleaseLine(line, "ID_LIKE"); ok {
			result.IDLike = strings.Fields(value)
		}
	}
	return result
}

func DetectMise(execPath string) bool {
	return regexp.MustCompile(`[/\\]mise[/\\]installs[/\\]`).MatchString(execPath)
}

func DetectAsdf(execPath string) bool {
	return regexp.MustCompile(`[/\\]\.?asdf[/\\]installs[/\\]`).MatchString(execPath)
}

func DetectHomebrew(execPath string) bool {
	return strings.Contains(execPath, "/Caskroom/")
}

func DetectWinget(execPath string) bool {
	return regexp.MustCompile(`Microsoft[/\\]WinGet[/\\](Packages|Links)`).MatchString(execPath)
}

func GuessLinuxPackageManager(execPath string, osRelease OSRelease) PackageManager {
	switch {
	case DetectMise(execPath):
		return PMMise
	case DetectAsdf(execPath):
		return PMAsdf
	case isDistroFamily(osRelease, "arch"):
		return PMPacman
	case isDistroFamily(osRelease, "debian"):
		return PMDeb
	case isDistroFamily(osRelease, "rhel", "fedora", "centos"):
		return PMRPM
	case isDistroFamily(osRelease, "alpine"):
		return PMApk
	default:
		return PMUnknown
	}
}

func DetectFromExecPath(execPath string, osRelease *OSRelease) PackageManager {
	switch {
	case DetectMise(execPath):
		return PMMise
	case DetectAsdf(execPath):
		return PMAsdf
	case DetectHomebrew(execPath):
		return PMHomebrew
	case DetectWinget(execPath):
		return PMWinget
	case osRelease != nil:
		return GuessLinuxPackageManager(execPath, *osRelease)
	default:
		return PMUnknown
	}
}

func LoadOSRelease(path string) (*OSRelease, error) {
	if path == "" {
		path = "/etc/os-release"
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	result := ParseOSRelease(string(data))
	return &result, nil
}

func parseOSReleaseLine(line string, key string) (string, bool) {
	prefix := key + "="
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(line, prefix)
	return strings.Trim(value, `"'`), true
}

func isDistroFamily(release OSRelease, families ...string) bool {
	for _, family := range families {
		if release.ID == family {
			return true
		}
		for _, like := range release.IDLike {
			if like == family {
				return true
			}
		}
	}
	return false
}
