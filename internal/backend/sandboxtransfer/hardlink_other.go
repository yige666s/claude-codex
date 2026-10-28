//go:build !linux

package sandboxtransfer

import "os"

// Linux broker/Node enforce the link-count check; other systems have no Node deployment.
func checkHardlink(os.FileInfo) error { return nil }
