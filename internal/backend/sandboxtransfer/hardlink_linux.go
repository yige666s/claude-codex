//go:build linux

package sandboxtransfer

import (
	"errors"
	"os"
	"syscall"
)

func checkHardlink(info os.FileInfo) error {
	if info.IsDir() {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return errors.New("task snapshot rejects hardlink")
	}
	return nil
}
