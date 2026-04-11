package mcp

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"
)

const redirectPortFallback = 3118

func BuildRedirectURI(port int) string {
	if port <= 0 {
		port = redirectPortFallback
	}
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

func FindAvailablePort() (int, error) {
	if configured := os.Getenv("MCP_OAUTH_CALLBACK_PORT"); configured != "" {
		if port, err := strconv.Atoi(configured); err == nil && port > 0 {
			return port, nil
		}
	}

	min, max := 49152, 65535
	if runtime.GOOS == "windows" {
		min, max = 39152, 49151
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for attempt := 0; attempt < 100; attempt++ {
		port := min + rng.Intn(max-min+1)
		if portAvailable(port) {
			return port, nil
		}
	}
	if portAvailable(redirectPortFallback) {
		return redirectPortFallback, nil
	}
	return 0, fmt.Errorf("no available ports for oauth redirect")
}

func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
