package voice

import (
	"os/exec"
	"runtime"
)

type DependencyStatus struct {
	Available      bool
	Missing        []string
	InstallCommand string
}

func CheckDependencies() DependencyStatus {
	if runtime.GOOS == "windows" {
		return DependencyStatus{Available: false, Missing: []string{"native audio module required"}}
	}
	if hasCommand("rec") {
		return DependencyStatus{Available: true}
	}
	if runtime.GOOS == "linux" && hasCommand("arecord") {
		return DependencyStatus{Available: true}
	}
	return DependencyStatus{
		Available:      false,
		Missing:        []string{"sox (rec command)"},
		InstallCommand: detectInstallCommand(),
	}
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectInstallCommand() string {
	switch {
	case hasCommand("brew"):
		return "brew install sox"
	case hasCommand("apt-get"):
		return "sudo apt-get install sox"
	case hasCommand("dnf"):
		return "sudo dnf install sox"
	case hasCommand("pacman"):
		return "sudo pacman -S sox"
	default:
		return ""
	}
}
