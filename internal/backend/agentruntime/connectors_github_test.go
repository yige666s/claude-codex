package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubIssueReaderWritesToolLedger(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh-test-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/repos/acme/agent/issues/7" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":70,"number":7,"title":"Wire connector reader","state":"open","html_url":"https://github.com/acme/agent/issues/7","body":"reader body","user":{"login":"alice"},"labels":[{"name":"connector"}],"created_at":"2026-06-20T01:02:03Z","updated_at":"2026-06-20T02:03:04Z"}`))
	}))
	defer api.Close()
	t.Setenv("GITHUB_API_BASE_URL", api.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	ledger := NewMemoryToolCallLedgerStore()
	runtime.SetToolCallLedgerStore(ledger)
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "test-token-ref", Provider: "github", AccessToken: "gh-test-token", TokenType: "bearer", Scopes: []string{"repo"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gh",
		UserID:           "alice",
		Provider:         "github",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"repo"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	issue, err := runtime.ReadGitHubIssue(context.Background(), "alice", "", "acme", "agent", 7)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if issue.Title != "Wire connector reader" || issue.Author != "alice" || strings.Join(issue.Labels, ",") != "connector" {
		data, _ := json.Marshal(issue)
		t.Fatalf("unexpected issue: %s", data)
	}
	entries, err := ledger.ListToolCalls(context.Background(), ToolCallLedgerFilter{UserID: "alice", ToolName: "github_issue_reader"})
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d", len(entries))
	}
	if entries[0].Status != "succeeded" || !strings.Contains(entries[0].Output, "Wire connector reader") {
		t.Fatalf("unexpected ledger entry: %#v", entries[0])
	}
}

func TestGitHubIssueReaderThroughMCPConnectorExecutor(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh-test-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"github_read_issue","description":"Read a GitHub issue","inputSchema":{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"issue_number":{"type":"integer"}},"required":["owner","repo","issue_number"]}}]}}`))
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode call params: %v", err)
			}
			if params.Name != MCPToolGitHubReadIssue {
				t.Fatalf("tool name = %q", params.Name)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"id\":90,\"number\":9,\"title\":\"MCP connector path\",\"state\":\"open\",\"html_url\":\"https://github.com/acme/agent/issues/9\",\"body\":\"mcp reader body\",\"author\":\"bob\",\"labels\":[\"mcp\"],\"created_at\":\"2026-06-20T01:02:03Z\",\"updated_at\":\"2026-06-20T02:03:04Z\"}"}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GITHUB_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GITHUB_MCP_SERVER_TRANSPORT", "http")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	ledger := NewMemoryToolCallLedgerStore()
	runtime.SetToolCallLedgerStore(ledger)
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "test-token-ref", Provider: "github", AccessToken: "gh-test-token", TokenType: "bearer", Scopes: []string{"repo"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gh",
		UserID:           "alice",
		Provider:         "github",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"repo"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	args := json.RawMessage(`{"user_id":"alice","owner":"acme","repo":"agent","issue_number":9}`)
	result, server, policy, err := runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "github",
		ToolName: MCPToolGitHubReadIssue,
		Args:     args,
	})
	if err != nil {
		t.Fatalf("mcp connector call: %v", err)
	}
	if server.Transport != "http" || server.Provider != "github" || server.URL != mcpServer.URL {
		t.Fatalf("unexpected server binding: %#v", server)
	}
	if policy.PermissionPolicy != ConnectorPolicyReadOnly || policy.SideEffectLevel != MCPToolSideEffectRead {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	var issue GitHubIssueInfo
	if err := json.Unmarshal([]byte(result.Output), &issue); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if issue.Title != "MCP connector path" || issue.Author != "bob" || strings.Join(issue.Labels, ",") != "mcp" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
	entries, err := ledger.ListToolCalls(context.Background(), ToolCallLedgerFilter{UserID: "alice", ToolName: "mcp.github." + MCPToolGitHubReadIssue})
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != "succeeded" {
		t.Fatalf("unexpected mcp ledger entries: %#v", entries)
	}
}

func TestGitHubRepositoryListerThroughMCPConnectorExecutor(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh-test-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"github_list_repositories","description":"List GitHub repositories","inputSchema":{"type":"object","properties":{"visibility":{"type":"string"},"limit":{"type":"integer"}}}}]}}`))
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode call params: %v", err)
			}
			if params.Name != MCPToolGitHubListRepositories {
				t.Fatalf("tool name = %q", params.Name)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"[{\"id\":1,\"full_name\":\"alice/public-one\",\"private\":false,\"html_url\":\"https://github.com/alice/public-one\",\"description\":\"first repo\",\"default_branch\":\"main\",\"stargazers_count\":3,\"forks_count\":1,\"open_issues_count\":2,\"updated_at\":\"2026-06-20T02:03:04Z\"},{\"id\":2,\"full_name\":\"acme/shared-tool\",\"private\":false,\"html_url\":\"https://github.com/acme/shared-tool\",\"description\":\"shared repo\",\"default_branch\":\"trunk\",\"stargazers_count\":5,\"forks_count\":0,\"open_issues_count\":0,\"updated_at\":\"2026-06-20T03:04:05Z\"}]"}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GITHUB_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GITHUB_MCP_SERVER_TRANSPORT", "http")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	ledger := NewMemoryToolCallLedgerStore()
	runtime.SetToolCallLedgerStore(ledger)
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "test-token-ref", Provider: "github", AccessToken: "gh-test-token", TokenType: "bearer", Scopes: []string{"repo"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gh",
		UserID:           "alice",
		Provider:         "github",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"repo"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	result, server, policy, err := runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "github",
		ToolName: MCPToolGitHubListRepositories,
		Args:     json.RawMessage(`{"user_id":"alice","visibility":"public","limit":25}`),
	})
	if err != nil {
		t.Fatalf("mcp connector call: %v", err)
	}
	if server.Transport != "http" || server.Provider != "github" || server.URL != mcpServer.URL {
		t.Fatalf("unexpected server binding: %#v", server)
	}
	if policy.PermissionPolicy != ConnectorPolicyReadOnly || policy.SideEffectLevel != MCPToolSideEffectRead {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	var repositories []GitHubRepositoryInfo
	if err := json.Unmarshal([]byte(result.Output), &repositories); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(repositories) != 2 || repositories[0].FullName != "alice/public-one" || repositories[1].FullName != "acme/shared-tool" {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	entries, err := ledger.ListToolCalls(context.Background(), ToolCallLedgerFilter{UserID: "alice", ToolName: "mcp.github." + MCPToolGitHubListRepositories})
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != "succeeded" {
		t.Fatalf("unexpected mcp ledger entries: %#v", entries)
	}
}

func TestGitHubConnectorMCPToolsExposeRepositoryLister(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"github_list_repositories","description":"List GitHub repositories","inputSchema":{"type":"object","properties":{"visibility":{"type":"string"},"limit":{"type":"integer"}}}},{"name":"github_read_repository","description":"Read GitHub repository","inputSchema":{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"}},"required":["owner","repo"]}},{"name":"github_read_issue","description":"Read GitHub issue","inputSchema":{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"issue_number":{"type":"integer"}},"required":["owner","repo","issue_number"]}}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GITHUB_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GITHUB_MCP_SERVER_TRANSPORT", "http")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "test-token-ref", Provider: "github", AccessToken: "gh-test-token", TokenType: "bearer", Scopes: []string{"repo"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gh",
		UserID:           "alice",
		Provider:         "github",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"repo"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	tools := runtime.ConnectorMCPTools(context.Background(), Scope{UserID: "alice", ConnectorContext: []string{"github"}})
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names[MCPToolGitHubListRepositories] || !names[MCPToolGitHubReadRepository] || !names[MCPToolGitHubReadIssue] {
		t.Fatalf("github runtime tool names = %#v", names)
	}
}

func TestGitHubConnectorMCPToolsSwitchesStaleBuiltinBindingToRemote(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search_repositories","description":"Search GitHub repositories","inputSchema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}]}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GITHUB_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GITHUB_MCP_SERVER_TRANSPORT", "http")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	mcpStore := NewMemoryMCPConnectorStore()
	runtime.SetMCPConnectorStore(mcpStore)
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "test-token-ref", Provider: "github", AccessToken: "gh-test-token", TokenType: "bearer", Scopes: []string{"repo"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gh",
		UserID:           "alice",
		Provider:         "github",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"repo"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	server, err := mcpStore.UpsertServer(context.Background(), MCPServerBinding{
		ID:               "mcp-gh",
		UserID:           "alice",
		Provider:         "github",
		DisplayName:      "GitHub",
		Transport:        "inprocess",
		OAuthTokenRef:    token.Ref,
		Status:           MCPServerStatusConnected,
		LastDiscoveredAt: &now,
		Metadata:         map[string]any{"connection_kind": MCPConnectionKindBuiltinAdapter, "connector_id": "conn-gh"},
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		t.Fatalf("upsert server: %v", err)
	}
	for _, toolName := range []string{MCPToolGitHubReadIssue, MCPToolGitHubReadRepository} {
		if _, err := mcpStore.UpsertToolPolicy(context.Background(), MCPToolPolicy{
			ID:               "pol-" + toolName,
			UserID:           "alice",
			ServerID:         server.ID,
			Provider:         "github",
			ToolName:         toolName,
			PermissionPolicy: ConnectorPolicyReadOnly,
			SideEffectLevel:  MCPToolSideEffectRead,
			Allowed:          true,
			CreatedAt:        now,
			UpdatedAt:        now,
		}); err != nil {
			t.Fatalf("upsert policy %s: %v", toolName, err)
		}
	}

	tools := runtime.ConnectorMCPTools(context.Background(), Scope{UserID: "alice", ConnectorContext: []string{"github"}})
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if names[MCPToolGitHubReadIssue] || names[MCPToolGitHubReadRepository] || !names["github_search_repositories"] {
		t.Fatalf("github runtime tool names = %#v", names)
	}
}

func TestRemoteMCPConnectorDiscoveryCreatesPolicies(t *testing.T) {
	sawAuthorization := false
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/initialize" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"linear-test","version":"test"}}`))
			return
		}
		if r.URL.Path != "/tools" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer linear-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		sawAuthorization = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tools":[{"name":"linear_read_issue","description":"Read Linear issue","input_schema":{"type":"object"}}]}`))
	}))
	defer mcpServer.Close()
	t.Setenv("LINEAR_MCP_SERVER_URL", mcpServer.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "linear-token-ref", Provider: "linear", AccessToken: "linear-token", TokenType: "Bearer", Scopes: []string{"read"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-linear",
		UserID:           "alice",
		Provider:         "linear",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           []string{"read"},
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	statuses, err := runtime.ListConnectorStatus(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("list connector status: %v", err)
	}
	if !sawAuthorization {
		t.Fatal("remote MCP discovery did not send authorization header")
	}
	var linear ConnectorStatus
	for _, status := range statuses {
		if status.Provider.ID == "linear" {
			linear = status
			break
		}
	}
	if linear.MCPServer == nil {
		t.Fatal("linear MCP server binding missing")
	}
	if linear.MCPServer.Transport != "sse" || linear.MCPServer.URL != mcpServer.URL || linear.MCPServer.Status != MCPServerStatusConnected {
		t.Fatalf("unexpected MCP server binding: %#v", linear.MCPServer)
	}
	if len(linear.MCPTools) != 1 || linear.MCPTools[0].ToolName != "linear_read_issue" || linear.MCPTools[0].PermissionPolicy != ConnectorPolicyReadOnly {
		t.Fatalf("unexpected MCP tool policies: %#v", linear.MCPTools)
	}
}

func TestRemoteMCPConnectorRequiresServerURL(t *testing.T) {
	t.Setenv("GMAIL_MCP_SERVER_URL", "")
	t.Setenv("AGENT_API_GMAIL_MCP_SERVER_URL", "")
	t.Setenv("GOOGLE_MCP_SERVER_URL", "")
	t.Setenv("AGENT_API_GOOGLE_MCP_SERVER_URL", "")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gmail",
		UserID:           "alice",
		Provider:         "gmail",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	statuses, err := runtime.ListConnectorStatus(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("list connector status: %v", err)
	}
	var gmail ConnectorStatus
	for _, status := range statuses {
		if status.Provider.ID == "gmail" {
			gmail = status
			break
		}
	}
	if gmail.Provider.ConnectionKind != MCPConnectionKindRemote || gmail.Provider.SyncedIndex {
		t.Fatalf("gmail provider should be remote MCP only: %#v", gmail.Provider)
	}
	if gmail.MCPServer == nil || gmail.MCPServer.Transport != "sse" || gmail.MCPServer.Status != MCPServerStatusError {
		t.Fatalf("unexpected gmail MCP server: %#v", gmail.MCPServer)
	}
	reason := deepAgentWorkflowString(gmail.MCPServer.Metadata, "last_discovery_error")
	if !strings.Contains(reason, "GMAIL_MCP_SERVER_URL") {
		t.Fatalf("missing URL reason = %q", reason)
	}
	_, _, _, err = runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "gmail",
		ToolName: "gmail_read_recent_messages",
		Args:     json.RawMessage(`{"query":"today"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "MCP server is not connected") {
		t.Fatalf("expected missing MCP server error, got %v", err)
	}
}

func TestRemoteMCPConnectorRetriesDiscoveryAfterError(t *testing.T) {
	var toolsRequests int
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"gmail-test","version":"test"}}`))
		case "/tools":
			toolsRequests++
			if toolsRequests == 1 {
				http.Error(w, "temporary discovery failure", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tools":[{"name":"gmail_read_recent_messages","description":"Read Gmail","input_schema":{"type":"object"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gmail",
		UserID:           "alice",
		Provider:         "gmail",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	first, err := runtime.ListConnectorStatus(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("first list connector status: %v", err)
	}
	if status := connectorStatusByProviderForTest(first, "gmail"); status.MCPServer == nil || status.MCPServer.Status != MCPServerStatusError {
		t.Fatalf("expected first discovery error, got %#v", status.MCPServer)
	}
	second, err := runtime.ListConnectorStatus(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("second list connector status: %v", err)
	}
	status := connectorStatusByProviderForTest(second, "gmail")
	if status.MCPServer == nil || status.MCPServer.Status != MCPServerStatusConnected {
		t.Fatalf("expected retry to connect MCP server, got %#v", status.MCPServer)
	}
	if toolsRequests != 2 {
		t.Fatalf("tools requests = %d, want 2", toolsRequests)
	}
}

func TestConnectorAuthorizationHeaderNormalizesSlackBotToken(t *testing.T) {
	header := connectorAuthorizationHeader(ConnectorToken{
		Provider:    "slack",
		AccessToken: "xoxb-test-token",
		TokenType:   "bot",
	})
	if header != "Bearer xoxb-test-token" {
		t.Fatalf("authorization header = %q", header)
	}
}

func connectorStatusByProviderForTest(statuses []ConnectorStatus, provider string) ConnectorStatus {
	for _, status := range statuses {
		if status.Provider.ID == provider {
			return status
		}
	}
	return ConnectorStatus{}
}

func TestGenericConnectorExecutorCallsRemoteMCPTool(t *testing.T) {
	sawAuthorization := false
	var sawInput map[string]any
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"gmail-test","version":"test"}}`))
		case "/tools":
			if r.Header.Get("Authorization") != "Bearer gmail-token" {
				t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
			}
			sawAuthorization = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tools":[{"name":"gmail_read_recent_messages","description":"Read recent Gmail messages","input_schema":{"type":"object"}}]}`))
		case "/call":
			if r.Header.Get("Authorization") != "Bearer gmail-token" {
				t.Fatalf("call authorization header = %q", r.Header.Get("Authorization"))
			}
			var body struct {
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode call: %v", err)
			}
			if body.Name != "gmail_read_recent_messages" {
				t.Fatalf("tool name = %q", body.Name)
			}
			if err := json.Unmarshal(body.Input, &sawInput); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":"邮件摘要：今天有 2 封新邮件。"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:                   "conn-gmail",
		UserID:               "alice",
		Provider:             "gmail",
		ExternalAccountLabel: "alice@example.com",
		Status:               ConnectorStatusConnected,
		PermissionPolicy:     ConnectorPolicyReadOnly,
		Scopes:               token.Scopes,
		TokenRef:             token.Ref,
		ConnectedAt:          &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	registry := NewRuntimeDeepAgentExecutorRegistry(runtime, nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "mail", Mode: DeepAgentToolModeConnector, Executor: deepAgentRouteExecutorConnector}, DeepAgentAction{
		StepID: "mail",
		Tool:   DeepAgentToolModeConnector,
		Args: map[string]any{
			"provider":  "gmail",
			"tool_name": "gmail_read_recent_messages",
			"tool_args": map[string]any{"query": "today"},
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice"}, Goal: "查看今天的新邮件"})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v evidence=%#v", err, evidence)
	}
	if !sawAuthorization {
		t.Fatal("remote MCP discovery did not send authorization header")
	}
	if sawInput["query"] != "today" {
		t.Fatalf("unexpected MCP input: %#v", sawInput)
	}
	if evidence.Output != "邮件摘要：今天有 2 封新邮件。" {
		t.Fatalf("unexpected output: %q", evidence.Output)
	}
	if len(evidence.ToolCalls) != 1 || evidence.ToolCalls[0].Name != "gmail_read_recent_messages" || evidence.ToolCalls[0].Status != DeepAgentActionStatusSucceeded {
		t.Fatalf("unexpected tool calls: %#v", evidence.ToolCalls)
	}
}

func TestRemoteMCPConnectorRefreshesStaleOAuthTokenRef(t *testing.T) {
	var sawDiscoveryWithNewToken bool
	var sawCallWithNewToken bool
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"gmail-test","version":"test"}}`))
		case "/tools":
			if got := r.Header.Get("Authorization"); got != "Bearer new-gmail-token" {
				t.Fatalf("discovery authorization header = %q", got)
			}
			sawDiscoveryWithNewToken = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tools":[{"name":"gmail_read_recent_messages","description":"Read recent Gmail messages","input_schema":{"type":"object"}}]}`))
		case "/call":
			if got := r.Header.Get("Authorization"); got != "Bearer new-gmail-token" {
				t.Fatalf("call authorization header = %q", got)
			}
			sawCallWithNewToken = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":"今天有 1 封新邮件。"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	mcpStore := NewMemoryMCPConnectorStore()
	runtime.SetMCPConnectorStore(mcpStore)
	now := time.Now().UTC()
	oldToken := ConnectorToken{Ref: "old-gmail-token-ref", Provider: "gmail", AccessToken: "old-gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	newToken := ConnectorToken{Ref: "new-gmail-token-ref", Provider: "gmail", AccessToken: "new-gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), oldToken); err != nil {
		t.Fatalf("put old token: %v", err)
	}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), newToken); err != nil {
		t.Fatalf("put new token: %v", err)
	}
	if _, err := mcpStore.UpsertServer(context.Background(), MCPServerBinding{
		ID:            "mcp-stale-gmail",
		UserID:        "alice",
		Provider:      "gmail",
		DisplayName:   "Gmail",
		Transport:     "http",
		URL:           mcpServer.URL,
		OAuthTokenRef: oldToken.Ref,
		Status:        MCPServerStatusConnected,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata: map[string]any{
			"connection_kind": MCPConnectionKindRemote,
			"connector_id":    "old-conn-gmail",
		},
	}); err != nil {
		t.Fatalf("upsert stale mcp server: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:                   "new-conn-gmail",
		UserID:               "alice",
		Provider:             "gmail",
		ExternalAccountLabel: "alice@example.com",
		Status:               ConnectorStatusConnected,
		PermissionPolicy:     ConnectorPolicyReadOnly,
		Scopes:               newToken.Scopes,
		TokenRef:             newToken.Ref,
		ConnectedAt:          &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	result, server, _, err := runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "gmail",
		ToolName: "gmail_read_recent_messages",
		Args:     json.RawMessage(`{"query":"today"}`),
	})
	if err != nil {
		t.Fatalf("mcp connector call: %v", err)
	}
	if server.OAuthTokenRef != newToken.Ref {
		t.Fatalf("server OAuthTokenRef = %q, want %q", server.OAuthTokenRef, newToken.Ref)
	}
	if !sawDiscoveryWithNewToken || !sawCallWithNewToken {
		t.Fatalf("new token not used for discovery/call: discovery=%v call=%v", sawDiscoveryWithNewToken, sawCallWithNewToken)
	}
	if result.Output != "今天有 1 封新邮件。" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestGmailMCPPermissionErrorDoesNotFallbackByDefault(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int64  `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search_threads","description":"Search Gmail threads","inputSchema":{"type":"object","properties":{"query":{"type":"string"}}}}]}}`))
		case "tools/call":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"The caller does not have permission"}],"isError":true}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GMAIL_MCP_SERVER_TRANSPORT", "http")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gmail",
		UserID:           "alice",
		Provider:         "gmail",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	_, _, _, err := runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "gmail",
		ToolName: "search_threads",
		Args:     json.RawMessage(`{"query":"after:2026/06/21"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "The caller does not have permission") {
		t.Fatalf("expected MCP permission error without REST fallback, got %v", err)
	}
}

func TestGmailMCPPermissionErrorFallsBackToGmailRESTWhenEnabled(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int64  `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search_threads","description":"Search Gmail threads","inputSchema":{"type":"object","properties":{"query":{"type":"string"}}}}]}}`))
		case "tools/call":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"The caller does not have permission"}],"isError":true}}`))
		default:
			t.Fatalf("unexpected MCP method %q", req.Method)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)
	t.Setenv("GMAIL_MCP_SERVER_TRANSPORT", "http")
	t.Setenv("AGENT_API_GMAIL_REST_FALLBACK_ENABLED", "true")

	gmailAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gmail-token" {
			t.Fatalf("gmail API authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/me/messages":
			if r.URL.Query().Get("q") != "after:2026/06/21" {
				t.Fatalf("query = %q", r.URL.Query().Get("q"))
			}
			_, _ = w.Write([]byte(`{"messages":[{"id":"msg-1","threadId":"thread-1"}],"resultSizeEstimate":1}`))
		case r.URL.Path == "/users/me/threads/thread-1":
			_, _ = w.Write([]byte(`{"id":"thread-1","messages":[{"id":"msg-1","threadId":"thread-1","snippet":"Build finished successfully","payload":{"headers":[{"name":"From","value":"ci@example.com"},{"name":"Subject","value":"Build update"},{"name":"Date","value":"Mon, 22 Jun 2026 10:00:00 +0000"}]}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gmailAPI.Close()
	t.Setenv("GMAIL_API_BASE_URL", gmailAPI.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-gmail",
		UserID:           "alice",
		Provider:         "gmail",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	result, _, _, err := runtime.CallConnectorMCPTool(context.Background(), MCPConnectorToolCall{
		UserID:   "alice",
		Provider: "gmail",
		ToolName: "search_threads",
		Args:     json.RawMessage(`{"query":"after:2026/06/21"}`),
	})
	if err != nil {
		t.Fatalf("mcp connector call: %v", err)
	}
	if !strings.Contains(result.Output, `"source":"gmail_rest_fallback"`) || !strings.Contains(result.Output, "Build update") {
		t.Fatalf("unexpected fallback output: %s", result.Output)
	}
}

func TestConnectorMCPToolsExposeCallableRuntimeTools(t *testing.T) {
	var sawInput map[string]any
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"gmail-test","version":"test"}}`))
		case "/tools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tools":[{"name":"search_threads","description":"Search Gmail threads","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}]}`))
		case "/call":
			var body struct {
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode call: %v", err)
			}
			if body.Name != "search_threads" {
				t.Fatalf("tool name = %q", body.Name)
			}
			if err := json.Unmarshal(body.Input, &sawInput); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":"今天有 2 封新邮件。"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mcpServer.Close()
	t.Setenv("GMAIL_MCP_SERVER_URL", mcpServer.URL)

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	runtime.SetMCPConnectorStore(NewMemoryMCPConnectorStore())
	now := time.Now().UTC()
	token := ConnectorToken{Ref: "gmail-token-ref", Provider: "gmail", AccessToken: "gmail-token", TokenType: "Bearer", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}, UpdatedAt: now}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:                   "conn-gmail",
		UserID:               "alice",
		Provider:             "gmail",
		ExternalAccountLabel: "alice@example.com",
		Status:               ConnectorStatusConnected,
		PermissionPolicy:     ConnectorPolicyReadOnly,
		Scopes:               token.Scopes,
		TokenRef:             token.Ref,
		ConnectedAt:          &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	tools := runtime.ConnectorMCPTools(context.Background(), Scope{UserID: "alice", ConnectorContext: []string{"gmail"}})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	if tools[0].Name() != "gmail_search_threads" {
		t.Fatalf("tool name = %q", tools[0].Name())
	}
	if !strings.Contains(tools[0].Description(), "Search Gmail threads") {
		t.Fatalf("description = %q", tools[0].Description())
	}
	result, err := tools[0].Execute(context.Background(), json.RawMessage(`{"query":"newer_than:1d"}`))
	if err != nil {
		t.Fatalf("execute runtime tool: %v", err)
	}
	if sawInput["query"] != "newer_than:1d" {
		t.Fatalf("unexpected MCP input: %#v", sawInput)
	}
	if result.Output != "今天有 2 封新邮件。" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestExternalConnectorOAuthCodeExchanges(t *testing.T) {
	type expectedTokenRequest struct {
		clientID     string
		clientSecret string
		code         string
		redirectURI  string
	}
	requests := map[string]expectedTokenRequest{}
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/google/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse google form: %v", err)
			}
			requests["google_drive"] = expectedTokenRequest{
				clientID:     r.Form.Get("client_id"),
				clientSecret: r.Form.Get("client_secret"),
				code:         r.Form.Get("code"),
				redirectURI:  r.Form.Get("redirect_uri"),
			}
			_, _ = w.Write([]byte(`{"access_token":"google-access","refresh_token":"google-refresh","token_type":"Bearer","scope":"openid email profile https://www.googleapis.com/auth/drive.readonly","expires_in":3600}`))
		case "/google/userinfo":
			if r.Header.Get("Authorization") != "Bearer google-access" {
				t.Fatalf("google authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"sub":"google-user-1","email":"alice@example.com","name":"Alice"}`))
		case "/notion/token":
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
				t.Fatalf("notion authorization header = %q", got)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode notion body: %v", err)
			}
			requests["notion"] = expectedTokenRequest{code: body["code"], redirectURI: body["redirect_uri"]}
			_, _ = w.Write([]byte(`{"access_token":"notion-access","token_type":"bearer","workspace_id":"notion-workspace","workspace_name":"Docs HQ","bot_id":"bot-1"}`))
		case "/slack/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse slack form: %v", err)
			}
			requests["slack"] = expectedTokenRequest{
				clientID:     r.Form.Get("client_id"),
				clientSecret: r.Form.Get("client_secret"),
				code:         r.Form.Get("code"),
				redirectURI:  r.Form.Get("redirect_uri"),
			}
			_, _ = w.Write([]byte(`{"ok":true,"team":{"id":"T1","name":"Team HQ"},"authed_user":{"id":"U1","access_token":"slack-user-access","token_type":"Bearer","scope":"search:read.public,search:read.private,search:read.mpim,search:read.im,search:read.files,search:read.users,files:read,emoji:read,channels:history,groups:history,mpim:history,im:history,users:read,users:read.email,channels:read,groups:read,mpim:read"}}`))
		case "/linear/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse linear form: %v", err)
			}
			requests["linear"] = expectedTokenRequest{
				clientID:     r.Form.Get("client_id"),
				clientSecret: r.Form.Get("client_secret"),
				code:         r.Form.Get("code"),
				redirectURI:  r.Form.Get("redirect_uri"),
			}
			_, _ = w.Write([]byte(`{"access_token":"linear-access","refresh_token":"linear-refresh","token_type":"Bearer","scope":["read","write"],"expires_in":3600}`))
		case "/linear/graphql":
			if r.Header.Get("Authorization") != "Bearer linear-access" {
				t.Fatalf("linear authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"linear-user-1","name":"Alice Linear","email":"linear@example.com"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer tokenServer.Close()

	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "google-client")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "google-secret")
	t.Setenv("GOOGLE_DRIVE_OAUTH_TOKEN_URL", tokenServer.URL+"/google/token")
	t.Setenv("GOOGLE_OAUTH_USERINFO_URL", tokenServer.URL+"/google/userinfo")
	t.Setenv("NOTION_OAUTH_CLIENT_ID", "notion-client")
	t.Setenv("NOTION_OAUTH_CLIENT_SECRET", "notion-secret")
	t.Setenv("NOTION_OAUTH_TOKEN_URL", tokenServer.URL+"/notion/token")
	t.Setenv("SLACK_CLIENT_ID", "slack-client")
	t.Setenv("SLACK_CLIENT_SECRET", "slack-secret")
	t.Setenv("SLACK_OAUTH_TOKEN_URL", tokenServer.URL+"/slack/token")
	t.Setenv("LINEAR_CLIENT_ID", "linear-client")
	t.Setenv("LINEAR_CLIENT_SECRET", "linear-secret")
	t.Setenv("LINEAR_OAUTH_TOKEN_URL", tokenServer.URL+"/linear/token")
	t.Setenv("LINEAR_GRAPHQL_URL", tokenServer.URL+"/linear/graphql")

	tests := []struct {
		provider   string
		code       string
		wantToken  string
		wantLabel  string
		wantID     string
		wantScopes []string
	}{
		{provider: "google_drive", code: "google-code", wantToken: "google-access", wantLabel: "alice@example.com", wantID: "google-user-1", wantScopes: []string{"openid", "email", "profile", "https://www.googleapis.com/auth/drive.readonly"}},
		{provider: "notion", code: "notion-code", wantToken: "notion-access", wantLabel: "Docs HQ", wantID: "notion-workspace", wantScopes: []string{"read_content"}},
		{provider: "slack", code: "slack-code", wantToken: "slack-user-access", wantLabel: "Team HQ", wantID: "U1", wantScopes: []string{"search:read.public", "search:read.private", "search:read.mpim", "search:read.im", "search:read.files", "search:read.users", "files:read", "emoji:read", "channels:history", "groups:history", "mpim:history", "im:history", "users:read", "users:read.email", "channels:read", "groups:read", "mpim:read"}},
		{provider: "linear", code: "linear-code", wantToken: "linear-access", wantLabel: "linear@example.com", wantID: "linear-user-1", wantScopes: []string{"read", "write"}},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
			runtime.SetConnectorStore(NewMemoryConnectorStore())
			start, err := runtime.StartConnectorAuth(context.Background(), "alice", "", tt.provider, "http://localhost/callback", nil)
			if err != nil {
				t.Fatalf("start auth: %v", err)
			}
			connection, err := runtime.CompleteConnectorAuth(context.Background(), "alice", "", tt.provider, start.State, tt.code, "", "", nil)
			if err != nil {
				t.Fatalf("complete auth: %v", err)
			}
			if connection.ExternalAccountLabel != tt.wantLabel || connection.ExternalAccountID != tt.wantID {
				t.Fatalf("unexpected account: id=%q label=%q", connection.ExternalAccountID, connection.ExternalAccountLabel)
			}
			token, err := runtime.connectorTokenVault().GetToken(context.Background(), connection.TokenRef)
			if err != nil {
				t.Fatalf("get token: %v", err)
			}
			if token == nil || token.AccessToken != tt.wantToken || token.AccessToken == tt.code {
				t.Fatalf("unexpected token: %#v", token)
			}
			if strings.Join(token.Scopes, ",") != strings.Join(tt.wantScopes, ",") {
				t.Fatalf("scopes = %v, want %v", token.Scopes, tt.wantScopes)
			}
			req := requests[tt.provider]
			if req.code != tt.code || req.redirectURI != "http://localhost/callback" {
				t.Fatalf("unexpected token request: %#v", req)
			}
		})
	}
}

func TestConnectorAuthURLProviderScopeEncoding(t *testing.T) {
	t.Setenv("LINEAR_CLIENT_ID", "linear-client")
	t.Setenv("LINEAR_CLIENT_SECRET", "linear-secret")
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	start, err := runtime.
		StartConnectorAuth(context.Background(), "alice", "", "linear", "http://localhost/callback", nil)
	if err != nil {
		t.Fatalf("start linear auth: %v", err)
	}
	if !strings.Contains(start.AuthURL, "scope=read%2Cwrite") {
		t.Fatalf("linear auth URL should use comma-separated scopes: %s", start.AuthURL)
	}

	t.Setenv("SLACK_CLIENT_ID", "slack-client")
	t.Setenv("SLACK_CLIENT_SECRET", "slack-secret")
	start, err = runtime.StartConnectorAuth(context.Background(), "alice", "", "slack", "http://localhost/callback", nil)
	if err != nil {
		t.Fatalf("start slack auth: %v", err)
	}
	if !strings.Contains(start.AuthURL, "scope=search%3Aread.public%2Csearch%3Aread.private") {
		t.Fatalf("slack user OAuth URL should use comma-separated scopes: %s", start.AuthURL)
	}
	if strings.Contains(start.AuthURL, "user_scope=") {
		t.Fatalf("slack v2_user OAuth URL should not use user_scope: %s", start.AuthURL)
	}
	if !strings.HasPrefix(start.AuthURL, "https://slack.com/oauth/v2_user/authorize?") {
		t.Fatalf("slack auth URL should use user token OAuth endpoint: %s", start.AuthURL)
	}
}

func TestRefreshDueConnectorTokensRefreshesExternalOAuthTokens(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/google/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("unexpected refresh form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-google-access","refresh_token":"new-refresh","token_type":"Bearer","scope":"openid email profile https://www.googleapis.com/auth/drive.readonly","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "google-client")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "google-secret")
	t.Setenv("GOOGLE_DRIVE_OAUTH_TOKEN_URL", tokenServer.URL+"/google/token")

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	now := time.Now().UTC()
	oldExpires := now.Add(-time.Minute)
	token := ConnectorToken{
		Ref:          "google-token-ref",
		Provider:     "google_drive",
		AccessToken:  "old-google-access",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		Scopes:       []string{"openid", "email"},
		ExpiresAt:    &oldExpires,
		UpdatedAt:    now.Add(-time.Hour),
	}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-google",
		UserID:           "alice",
		Provider:         "google_drive",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		ExpiresAt:        &oldExpires,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	count, err := runtime.RefreshDueConnectorTokens(context.Background(), time.Minute, 10)
	if err != nil {
		t.Fatalf("refresh tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("refreshed count = %d", count)
	}
	connection, err := runtime.connectorStore().GetConnection(context.Background(), "alice", "", "google_drive")
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if connection == nil || connection.TokenRef == token.Ref || connection.Status != ConnectorStatusConnected {
		t.Fatalf("unexpected refreshed connection: %#v", connection)
	}
	refreshed, err := runtime.connectorTokenVault().GetToken(context.Background(), connection.TokenRef)
	if err != nil {
		t.Fatalf("get refreshed token: %v", err)
	}
	if refreshed == nil || refreshed.AccessToken != "new-google-access" || refreshed.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected refreshed token: %#v", refreshed)
	}
}

func TestRefreshDueConnectorTokensRefreshesNotionMCPDynamicOAuth(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notion/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-notion-refresh" {
			t.Fatalf("refresh_token = %q", got)
		}
		if got := r.Form.Get("client_id"); got != "dynamic-notion-client" {
			t.Fatalf("client_id = %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret should not be sent for Notion MCP refresh, got %q", got)
		}
		if got := r.Form.Get("resource"); got != "https://mcp.notion.com/mcp" {
			t.Fatalf("resource = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-notion-access","refresh_token":"new-notion-refresh","token_type":"bearer","expires_in":3600,"refresh_token_expires_in":86400}`))
	}))
	defer tokenServer.Close()

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	now := time.Now().UTC()
	oldExpires := now.Add(-time.Minute)
	token := ConnectorToken{
		Ref:          "notion-token-ref",
		Provider:     "notion",
		AccessToken:  "old-notion-access",
		RefreshToken: "old-notion-refresh",
		TokenType:    "bearer",
		Scopes:       []string{"read_content"},
		ExpiresAt:    &oldExpires,
		UpdatedAt:    now.Add(-time.Hour),
	}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-notion",
		UserID:           "alice",
		Provider:         "notion",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		Scopes:           token.Scopes,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		ExpiresAt:        &oldExpires,
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata: map[string]any{
			"oauth_mode":      "mcp",
			"oauth_client_id": "dynamic-notion-client",
			"token_endpoint":  tokenServer.URL + "/notion/token",
			"resource":        "https://mcp.notion.com/mcp",
		},
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	count, err := runtime.RefreshDueConnectorTokens(context.Background(), time.Minute, 10)
	if err != nil {
		t.Fatalf("refresh tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("refreshed count = %d", count)
	}
	connection, err := runtime.connectorStore().GetConnection(context.Background(), "alice", "", "notion")
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if connection == nil || connection.TokenRef == token.Ref || connection.Status != ConnectorStatusConnected {
		t.Fatalf("unexpected refreshed connection: %#v", connection)
	}
	refreshed, err := runtime.connectorTokenVault().GetToken(context.Background(), connection.TokenRef)
	if err != nil {
		t.Fatalf("get refreshed token: %v", err)
	}
	if refreshed == nil || refreshed.AccessToken != "new-notion-access" || refreshed.RefreshToken != "new-notion-refresh" {
		t.Fatalf("unexpected refreshed token: %#v", refreshed)
	}
	if refreshed.RefreshExpiresAt == nil || !refreshed.RefreshExpiresAt.After(now) {
		t.Fatalf("refresh token expiry not stored: %#v", refreshed)
	}
}

func TestMCPRuntimeConfigRefreshesExpiredConnectorTokenBeforeUse(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-mcp-access","refresh_token":"fresh-mcp-refresh","token_type":"bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	now := time.Now().UTC()
	oldExpires := now.Add(-time.Minute)
	token := ConnectorToken{
		Ref:          "old-mcp-token-ref",
		Provider:     "notion",
		AccessToken:  "stale-mcp-access",
		RefreshToken: "stale-mcp-refresh",
		TokenType:    "bearer",
		ExpiresAt:    &oldExpires,
		UpdatedAt:    now.Add(-time.Hour),
	}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-notion",
		UserID:           "alice",
		WorkspaceID:      "workspace-1",
		Provider:         "notion",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		ExpiresAt:        &oldExpires,
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata: map[string]any{
			"oauth_mode":      "mcp",
			"oauth_client_id": "dynamic-notion-client",
			"token_endpoint":  tokenServer.URL,
			"resource":        "https://mcp.notion.com/mcp",
		},
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	cfg, err := runtime.mcpRuntimeConfigForServer(context.Background(), MCPServerBinding{
		ID:            "mcp-notion",
		UserID:        "alice",
		WorkspaceID:   "workspace-1",
		Provider:      "notion",
		Transport:     "http",
		URL:           "https://mcp.notion.com/mcp",
		OAuthTokenRef: token.Ref,
	})
	if err != nil {
		t.Fatalf("runtime config: %v", err)
	}
	if got := cfg.Headers["Authorization"]; got != "Bearer fresh-mcp-access" {
		t.Fatalf("authorization header = %q", got)
	}
}

func TestRefreshDueConnectorTokensExpiresConnectionOnInvalidGrant(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
	}))
	defer tokenServer.Close()

	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetConnectorStore(NewMemoryConnectorStore())
	now := time.Now().UTC()
	oldExpires := now.Add(-time.Minute)
	token := ConnectorToken{
		Ref:          "expired-notion-token-ref",
		Provider:     "notion",
		AccessToken:  "old-notion-access",
		RefreshToken: "expired-notion-refresh",
		TokenType:    "bearer",
		ExpiresAt:    &oldExpires,
		UpdatedAt:    now.Add(-time.Hour),
	}
	if err := runtime.connectorTokenVault().PutToken(context.Background(), token); err != nil {
		t.Fatalf("put token: %v", err)
	}
	if _, err := runtime.connectorStore().UpsertConnection(context.Background(), ConnectorConnection{
		ID:               "conn-expired-notion",
		UserID:           "alice",
		Provider:         "notion",
		Status:           ConnectorStatusConnected,
		PermissionPolicy: ConnectorPolicyReadOnly,
		TokenRef:         token.Ref,
		ConnectedAt:      &now,
		ExpiresAt:        &oldExpires,
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata: map[string]any{
			"oauth_mode":      "mcp",
			"oauth_client_id": "dynamic-notion-client",
			"token_endpoint":  tokenServer.URL,
			"resource":        "https://mcp.notion.com/mcp",
		},
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	count, err := runtime.RefreshDueConnectorTokens(context.Background(), time.Minute, 10)
	if err != nil {
		t.Fatalf("refresh tokens: %v", err)
	}
	if count != 0 {
		t.Fatalf("refreshed count = %d", count)
	}
	connection, err := runtime.connectorStore().GetConnection(context.Background(), "alice", "", "notion")
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if connection == nil || connection.Status != ConnectorStatusExpired {
		t.Fatalf("connection should be expired after invalid_grant: %#v", connection)
	}
	if got := deepAgentWorkflowString(connection.Metadata, "last_refresh_error"); !strings.Contains(got, "invalid_grant") {
		t.Fatalf("last_refresh_error = %q", got)
	}
}
