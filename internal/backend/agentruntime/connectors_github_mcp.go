package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	MCPToolGitHubListRepositories = "github_list_repositories"
	MCPToolGitHubReadIssue        = "github_read_issue"
	MCPToolGitHubReadRepository   = "github_read_repository"
)

type gitHubConnectorMCPExecutor struct {
	runtime *Runtime
}

type gitHubMCPArgs struct {
	UserID      string `json:"user_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Repo        string `json:"repo,omitempty"`
	IssueNumber int    `json:"issue_number,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

func NewGitHubConnectorMCPServer(runtime *Runtime) *mcpcore.Server {
	return &mcpcore.Server{
		Name:         "agentapi-github-connector",
		Version:      "0.1.0",
		Instructions: "Read GitHub repository and issue context for AgentAPI connector evidence.",
		Executor:     gitHubConnectorMCPExecutor{runtime: runtime},
	}
}

func (e gitHubConnectorMCPExecutor) Descriptors() []toolkit.Descriptor {
	return []toolkit.Descriptor{
		{
			Name:        MCPToolGitHubListRepositories,
			Description: "List repositories visible to the connected GitHub account. Defaults to public repositories.",
			Permission:  permissions.LevelRead,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"},"workspace_id":{"type":"string"},"visibility":{"type":"string","enum":["public","private","all"],"description":"Repository visibility to list. Defaults to public."},"limit":{"type":"integer","minimum":1,"maximum":100,"description":"Maximum repositories to return. Defaults to 50."}}}`),
		},
		{
			Name:        MCPToolGitHubReadIssue,
			Description: "Read a GitHub issue using the connected user's GitHub connector.",
			Permission:  permissions.LevelRead,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"},"workspace_id":{"type":"string"},"owner":{"type":"string"},"repo":{"type":"string"},"issue_number":{"type":"integer","minimum":1}},"required":["owner","repo","issue_number"]}`),
		},
		{
			Name:        MCPToolGitHubReadRepository,
			Description: "Read GitHub repository metadata using the connected user's GitHub connector.",
			Permission:  permissions.LevelRead,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string"},"workspace_id":{"type":"string"},"owner":{"type":"string"},"repo":{"type":"string"}},"required":["owner","repo"]}`),
		},
	}
}

func (e gitHubConnectorMCPExecutor) Execute(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	if e.runtime == nil {
		return toolkit.Result{}, fmt.Errorf("github connector MCP adapter is not configured")
	}
	var args gitHubMCPArgs
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return toolkit.Result{}, err
		}
	}
	args.UserID = strings.TrimSpace(args.UserID)
	args.WorkspaceID = strings.TrimSpace(args.WorkspaceID)
	args.Owner, args.Repo = normalizeGitHubOwnerRepo(args.Owner, args.Repo)
	if args.UserID == "" {
		return toolkit.Result{}, fmt.Errorf("github MCP adapter requires user_id")
	}
	switch name {
	case MCPToolGitHubListRepositories:
		repositories, err := e.runtime.ListGitHubRepositories(ctx, args.UserID, args.WorkspaceID, args.Visibility, args.Limit)
		if err != nil {
			return toolkit.Result{}, err
		}
		data, _ := json.Marshal(repositories)
		return toolkit.Result{Output: string(data)}, nil
	case MCPToolGitHubReadIssue:
		if args.Owner == "" || args.Repo == "" || args.IssueNumber <= 0 {
			return toolkit.Result{}, fmt.Errorf("github_read_issue requires owner, repo, and positive issue_number")
		}
		issue, err := e.runtime.ReadGitHubIssue(ctx, args.UserID, args.WorkspaceID, args.Owner, args.Repo, args.IssueNumber)
		if err != nil {
			return toolkit.Result{}, err
		}
		data, _ := json.Marshal(issue)
		return toolkit.Result{Output: string(data)}, nil
	case MCPToolGitHubReadRepository:
		if args.Owner == "" || args.Repo == "" {
			return toolkit.Result{}, fmt.Errorf("github_read_repository requires owner and repo")
		}
		repository, err := e.runtime.ReadGitHubRepository(ctx, args.UserID, args.WorkspaceID, args.Owner, args.Repo)
		if err != nil {
			return toolkit.Result{}, err
		}
		data, _ := json.Marshal(repository)
		return toolkit.Result{Output: string(data)}, nil
	default:
		return toolkit.Result{}, fmt.Errorf("unknown github MCP connector tool %q", name)
	}
}
