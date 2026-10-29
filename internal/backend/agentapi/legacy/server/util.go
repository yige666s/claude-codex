package server

import "time"

// timeNowUnixMilli returns current Unix time in milliseconds
// This is a helper to maintain compatibility across Go versions
func timeNowUnixMilli() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
