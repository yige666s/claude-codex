//go:build !windows

package skills

import "syscall"

func noFollowFlag() int { return syscall.O_NOFOLLOW }
