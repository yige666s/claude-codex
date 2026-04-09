# CCR (Claude Code Remote) Server Architecture

## Overview

CCR is Anthropic's cloud container service that provides remote development environments for Claude Code. The server manages containerized sessions with PTY access, WebSocket communication, and multi-provider authentication.

## Core Components

### 1. Session Management

**SessionManager** (`internal/server/session.go`)
- Creates and manages remote development sessions
- Tracks session lifecycle: starting → running → detached → stopping → stopped
- Implements grace periods for reconnection (default: 5 minutes)
- Handles session cleanup and resource management
- Enforces per-user session limits

**Session State Machine:**
```
starting → running → detached → stopping → stopped
              ↓          ↑
              └──────────┘ (reconnect within grace period)
```

**Key Features:**
- Session persistence across disconnections
- Automatic cleanup after grace period expires
- Session metadata tracking (user, workspace, timestamps)
- Thread-safe session operations

### 2. PTY Server

**PTYServer** (`internal/server/pty.go`)
- Manages pseudo-terminal instances for each session
- Handles terminal I/O with scrollback buffer
- Supports terminal resizing
- Implements graceful shutdown

**Scrollback Buffer:**
- Configurable size (default: 1000 lines)
- Preserves terminal output for reconnecting clients
- Efficient circular buffer implementation

### 3. WebSocket Communication

**WebSocketHandler** (`internal/server/websocket.go`)
- Real-time bidirectional communication
- Message types: input, output, resize, ping/pong
- Automatic reconnection support
- Heartbeat mechanism for connection health

**Message Protocol:**
```json
{
  "type": "input|output|resize|ping|pong",
  "data": "...",
  "cols": 80,
  "rows": 24
}
```

### 4. Authentication System

**Multi-Provider Architecture:**

The auth system uses an adapter pattern to support multiple authentication methods:

```go
type AuthAdapter interface {
    SetupRoutes(mux *http.ServeMux)
    RequireAuth(next http.HandlerFunc) http.HandlerFunc
    GetUser(r *http.Request) (*AuthUser, error)
}
```

**Supported Providers:**

1. **Token Auth** (`auth/token_auth.go`)
   - Simple bearer token authentication
   - Supports Authorization header or query parameter
   - No-auth mode when AUTH_TOKEN not set
   - Best for: development, CI/CD, single-user deployments

2. **API Key Auth** (`auth/apikey_auth.go`)
   - Users provide their own Anthropic API keys
   - Keys stored in session (format: `sk-ant-*`)
   - Per-user API key management
   - Best for: multi-tenant deployments, BYOK scenarios

3. **OAuth Auth** (`auth/oauth_auth.go`)
   - Standard OAuth 2.0 flow
   - Authorization code grant
   - Callback handling and token exchange
   - Best for: enterprise SSO, third-party identity providers

**Session Store** (`auth/session.go`)
- Secure session token generation (32-byte random)
- Cookie-based session management
- Configurable session duration
- Thread-safe session operations

**Admin Users:**
- Configured via `ADMIN_USERS` environment variable
- Comma-separated email list
- Access to admin endpoints

### 5. Rate Limiting

**RateLimiter** (`internal/server/ratelimit.go`)
- Per-user rate limiting
- Per-IP rate limiting
- Configurable limits (requests per hour)
- Sliding window algorithm
- Automatic cleanup of expired entries

**Default Limits:**
- 100 requests per hour per user
- 200 requests per hour per IP

### 6. User Management

**UserStore** (`internal/server/user.go`)
- Tracks active users and their sessions
- Per-user session counting
- User statistics and metrics
- Thread-safe operations

### 7. Admin Interface

**AdminHandler** (`internal/server/admin.go`)
- Admin-only endpoints for monitoring
- Session listing and statistics
- User management
- Server health metrics

**Admin Endpoints:**
- `GET /admin/sessions` - List all active sessions
- `GET /admin/users` - List all users with session counts
- `GET /admin/stats` - Server statistics

### 8. Direct Connect Protocol

**ConnectHandler** (`internal/server/connect.go`)
- Entry point for remote session creation
- Returns session ID and WebSocket URL
- Workspace initialization
- Session metadata setup

**Connect Flow:**
```
1. Client → POST /connect
2. Server validates auth
3. Server creates session
4. Server returns {session_id, ws_url, work_dir}
5. Client → WebSocket connection to ws_url
6. PTY session established
```

## Configuration

**Environment Variables:**

```bash
# Server
PORT=8080
HOST=localhost
UNIX=/tmp/ccr.sock  # Unix socket (optional)

# Auth
AUTH_MODE=token|apikey|oauth
AUTH_TOKEN=secret123
ADMIN_USERS=admin@example.com,user@example.com

# OAuth (if AUTH_MODE=oauth)
OAUTH_CLIENT_ID=...
OAUTH_CLIENT_SECRET=...
OAUTH_REDIRECT_URI=https://example.com/auth/callback

# Session
IDLE_TIMEOUT_MS=300000  # 5 minutes
MAX_SESSIONS=100
WORKSPACE=/workspace

# Rate Limiting
RATE_LIMIT_PER_USER=100
RATE_LIMIT_PER_IP=200
```

## Security Features

1. **Authentication Required:**
   - All endpoints protected by auth middleware
   - Multiple auth provider support
   - Secure session token generation

2. **Rate Limiting:**
   - Prevents abuse and DoS attacks
   - Per-user and per-IP limits
   - Configurable thresholds

3. **Session Isolation:**
   - Each session runs in isolated PTY
   - Workspace isolation
   - User-specific session tracking

4. **Admin Access Control:**
   - Admin-only endpoints
   - Email-based admin list
   - Separate permission checks

5. **Secure Defaults:**
   - HTTPS recommended for production
   - Secure cookie flags
   - Token validation

## API Endpoints

### Public Endpoints

```
POST /connect
  - Create new remote session
  - Auth: Required
  - Returns: {session_id, ws_url, work_dir}

GET /ws/{session_id}
  - WebSocket connection for session
  - Auth: Required
  - Protocol: WebSocket

POST /auth/login
  - Login endpoint (varies by auth mode)
  - Auth: None
  - Returns: Session cookie

POST /auth/logout
  - Logout endpoint
  - Auth: Required
  - Returns: Success status
```

### Admin Endpoints

```
GET /admin/sessions
  - List all active sessions
  - Auth: Admin required
  - Returns: Array of session objects

GET /admin/users
  - List all users with stats
  - Auth: Admin required
  - Returns: {users, totalUsers}

GET /admin/stats
  - Server statistics
  - Auth: Admin required
  - Returns: {activeSessions, activeUsers, maxSessions}
```

## Message Flow

### Session Creation

```
Client                    Server                    PTY
  |                         |                        |
  |-- POST /connect ------->|                        |
  |                         |-- Create Session ----->|
  |                         |<-- Session Ready ------|
  |<-- {session_id, ws} ----|                        |
  |                         |                        |
  |-- WS Connect ---------->|                        |
  |                         |-- Attach PTY --------->|
  |<-- Terminal Output -----|<-- PTY Output ---------|
  |-- Terminal Input ------>|-- PTY Input ---------->|
```

### Reconnection

```
Client                    Server                    PTY
  |                         |                        |
  |-- WS Disconnect ------->|                        |
  |                         |-- Mark Detached ------>|
  |                         |   (grace period)       |
  |                         |                        |
  |-- WS Reconnect -------->|                        |
  |                         |-- Reattach PTY ------->|
  |<-- Scrollback Buffer ---|<-- Buffered Output ----|
  |<-- Live Output ---------|<-- PTY Output ---------|
```

## Deployment Considerations

1. **Container Orchestration:**
   - Each session runs in isolated container
   - Resource limits per container
   - Container lifecycle management

2. **Scaling:**
   - Horizontal scaling with session affinity
   - Load balancer with sticky sessions
   - Shared session store for multi-instance

3. **Monitoring:**
   - Session metrics and statistics
   - Rate limit tracking
   - Error logging and alerting

4. **Backup and Recovery:**
   - Session state persistence
   - Workspace snapshots
   - Graceful shutdown handling

## Future Enhancements

1. **Session Persistence:**
   - Database-backed session store
   - Cross-instance session migration
   - Long-term session storage

2. **Advanced Rate Limiting:**
   - Token bucket algorithm
   - Burst allowance
   - Dynamic rate adjustment

3. **Enhanced Security:**
   - mTLS support
   - API key rotation
   - Audit logging

4. **Performance:**
   - Connection pooling
   - Message compression
   - Optimized scrollback buffer

5. **Observability:**
   - Prometheus metrics
   - Distributed tracing
   - Health check endpoints
