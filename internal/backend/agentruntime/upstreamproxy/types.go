package upstreamproxy

import "net"

// State represents the upstreamproxy state
type State struct {
	Enabled      bool
	Port         int
	CABundlePath string
}

// Relay represents a running upstreamproxy relay
type Relay struct {
	Port int
	Stop func()
}

// RelayOptions contains options for starting the relay
type RelayOptions struct {
	WSUrl     string
	SessionID string
	Token     string
}

// InitOptions contains options for initializing upstreamproxy
type InitOptions struct {
	TokenPath    string
	SystemCAPath string
	CABundlePath string
	CCRBaseURL   string
}

// ConnState tracks the state of a single CONNECT tunnel
type ConnState struct {
	WS          WebSocketLike
	ConnectBuf  []byte
	Pinger      chan struct{} // channel to stop pinger
	Pending     [][]byte      // buffered data before WS opens
	WSOpen      bool
	Established bool // 200 Connection Established sent
	Closed      bool
}

// WebSocketLike is the minimal WebSocket interface needed by the relay
type WebSocketLike interface {
	Send(data []byte) error
	Close() error
	ReadyState() int
	SetBinaryType(t string)
}

// ClientSocket is a minimal socket abstraction
type ClientSocket interface {
	Write(data []byte) (int, error)
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

// Constants
const (
	// MaxChunkBytes is the Envoy per-request buffer cap
	MaxChunkBytes = 512 * 1024

	// PingIntervalMS is the keepalive ping interval (30 seconds)
	PingIntervalMS = 30000

	// SessionTokenPath is the default path to the session token
	SessionTokenPath = "/run/ccr/session_token"

	// SystemCABundle is the default system CA bundle path
	SystemCABundle = "/etc/ssl/certs/ca-certificates.crt"
)

// WebSocket ready states
const (
	WSConnecting = 0
	WSOpen       = 1
	WSClosing    = 2
	WSClosed     = 3
)

// NoProxyList is the list of hosts the proxy must NOT intercept
var NoProxyList = []string{
	"localhost",
	"127.0.0.1",
	"::1",
	"169.254.0.0/16",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"anthropic.com",
	".anthropic.com",
	"*.anthropic.com",
	"github.com",
	"api.github.com",
	"*.github.com",
	"*.githubusercontent.com",
	"registry.npmjs.org",
	"pypi.org",
	"files.pythonhosted.org",
	"index.crates.io",
	"proxy.golang.org",
}
