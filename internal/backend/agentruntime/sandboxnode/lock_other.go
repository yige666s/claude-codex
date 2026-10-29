//go:build !linux

package sandboxnode

import "errors"

type NodeLock struct{}

func AcquireNodeLock(string) (*NodeLock, error) { return nil, errors.New("Node lock requires Linux") }
func (*NodeLock) Close() error                  { return nil }
