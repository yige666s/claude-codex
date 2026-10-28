//go:build linux

package sandboxnode

import (
	"errors"
	"os"
	"syscall"
)

type NodeLock struct{ file *os.File }

// AcquireNodeLock fences a single process serving one private Node root.
// It must remain held until HTTP admission stops and all tasks are disposed.
func AcquireNodeLock(root string) (*NodeLock, error) {
	if err := validPrivateRoot(root); err != nil {
		return nil, err
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	prior, priorErr := directory.Lstat(".node.lock")
	if priorErr == nil && (!prior.Mode().IsRegular() || prior.Mode().Perm()&0077 != 0) {
		return nil, errors.New("Node lock entry is unsafe")
	}
	if priorErr != nil && !errors.Is(priorErr, os.ErrNotExist) {
		return nil, priorErr
	}
	file, err := directory.OpenFile(".node.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || (priorErr == nil && !os.SameFile(prior, info)) {
		file.Close()
		return nil, errors.New("Node lock file is not private and regular")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("Node root already locked")
	}
	return &NodeLock{file: file}, nil
}
func (l *NodeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
