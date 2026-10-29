package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	toolkit "claude-codex/internal/harness/tools"
)

type Authenticator struct {
	Secret string
}

func NewAuthenticator(secret string) *Authenticator {
	return &Authenticator{Secret: secret}
}

func (a *Authenticator) Validate(secret string) bool {
	return a == nil || a.Secret == "" || a.Secret == secret
}

type Runner interface {
	RunPrompt(context.Context, string, string) (string, error)
	ListTools(context.Context, string) ([]toolkit.Descriptor, error)
}

type SessionRunner interface {
	CreateSession(context.Context, string) (*SessionInfo, error)
	RunSessionPrompt(context.Context, string, string) (*SessionPromptResult, error)
	GetSession(context.Context, string) (*SessionInfo, error)
	ListSessions(context.Context, string) ([]SessionInfo, error)
	DeleteSession(context.Context, string) error
}

type Server struct {
	auth   *Authenticator
	runner Runner
}

func NewServer(secretOrAuth any, runner Runner) *Server {
	switch value := secretOrAuth.(type) {
	case nil:
		return &Server{runner: runner}
	case string:
		return &Server{auth: NewAuthenticator(value), runner: runner}
	case *Authenticator:
		return &Server{auth: value, runner: runner}
	default:
		return &Server{runner: runner}
	}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	decoder := json.NewDecoder(in)
	encoder := json.NewEncoder(out)
	for {
		var request Request
		if err := decoder.Decode(&request); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		response := s.handle(ctx, request)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, request Request) Response {
	response := Response{ID: request.ID}
	if !s.auth.Validate(request.Secret) {
		response.Error = "unauthorized"
		return response
	}

	switch request.Method {
	case MethodRunPrompt:
		args, err := resolveRunPromptArgs(request)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		result, err := s.runner.RunPrompt(ctx, args.WorkingDir, args.Prompt)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, RunPromptResult{Output: result})
	case MethodListTools:
		workingDir := resolveListToolsArgs(request)
		tools, err := s.runner.ListTools(ctx, workingDir)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, ListToolsResult{Tools: tools})
	case MethodCreateSession:
		runner, ok := s.runner.(SessionRunner)
		if !ok {
			response.Error = "create_session is not supported"
			return response
		}
		workingDir := resolveCreateSessionArgs(request)
		session, err := runner.CreateSession(ctx, workingDir)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, CreateSessionResult{Session: *session})
	case MethodSessionPrompt:
		runner, ok := s.runner.(SessionRunner)
		if !ok {
			response.Error = "session_prompt is not supported"
			return response
		}
		args, err := resolveSessionPromptArgs(request)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		result, err := runner.RunSessionPrompt(ctx, args.SessionID, args.Prompt)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, result)
	case MethodGetSession:
		runner, ok := s.runner.(SessionRunner)
		if !ok {
			response.Error = "get_session is not supported"
			return response
		}
		args, err := resolveSessionIDArgs(request)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		session, err := runner.GetSession(ctx, args.SessionID)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, GetSessionResult{Session: *session})
	case MethodListSessions:
		runner, ok := s.runner.(SessionRunner)
		if !ok {
			response.Error = "list_sessions is not supported"
			return response
		}
		workingDir := resolveListSessionsArgs(request)
		sessions, err := runner.ListSessions(ctx, workingDir)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, ListSessionsResult{Sessions: sessions})
	case MethodDeleteSession:
		runner, ok := s.runner.(SessionRunner)
		if !ok {
			response.Error = "delete_session is not supported"
			return response
		}
		args, err := resolveSessionIDArgs(request)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		if err := runner.DeleteSession(ctx, args.SessionID); err != nil {
			response.Error = err.Error()
			return response
		}
		mustWriteResult(&response, DeleteSessionResult{Deleted: true, SessionID: args.SessionID})
	default:
		response.Error = fmt.Sprintf("unknown method %s", request.Method)
	}
	return response
}

type runPromptArgs struct {
	WorkingDir string
	Prompt     string
}

type sessionPromptArgs struct {
	SessionID string
	Prompt    string
}

type sessionIDArgs struct {
	SessionID string
}

func resolveRunPromptArgs(request Request) (runPromptArgs, error) {
	workingDir := request.WorkingDir
	prompt := request.Prompt
	if len(request.Params) > 0 {
		var params runPromptParams
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if workingDir == "" {
				workingDir = params.WorkingDir
			}
			if prompt == "" {
				prompt = params.Prompt
			}
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return runPromptArgs{}, errors.New("prompt is required")
	}
	return runPromptArgs{WorkingDir: workingDir, Prompt: prompt}, nil
}

func resolveListToolsArgs(request Request) string {
	workingDir := request.WorkingDir
	if len(request.Params) > 0 {
		var params listToolsParams
		if err := json.Unmarshal(request.Params, &params); err == nil && workingDir == "" {
			workingDir = params.WorkingDir
		}
	}
	return workingDir
}

func resolveCreateSessionArgs(request Request) string {
	workingDir := request.WorkingDir
	if len(request.Params) > 0 {
		var params createSessionParams
		if err := json.Unmarshal(request.Params, &params); err == nil && workingDir == "" {
			workingDir = params.WorkingDir
		}
	}
	return workingDir
}

func resolveSessionPromptArgs(request Request) (sessionPromptArgs, error) {
	sessionID := request.SessionID
	prompt := request.Prompt
	if len(request.Params) > 0 {
		var params sessionPromptParams
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if sessionID == "" {
				sessionID = params.SessionID
			}
			if prompt == "" {
				prompt = params.Prompt
			}
		}
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(prompt) == "" {
		return sessionPromptArgs{}, errors.New("session_id and prompt are required")
	}
	return sessionPromptArgs{SessionID: sessionID, Prompt: prompt}, nil
}

func resolveSessionID(request Request) string {
	sessionID := request.SessionID
	if len(request.Params) == 0 || sessionID != "" {
		return sessionID
	}

	for _, params := range []any{&getSessionParams{}, &deleteSessionParams{}} {
		if err := json.Unmarshal(request.Params, params); err != nil {
			continue
		}
		switch value := params.(type) {
		case *getSessionParams:
			if value.SessionID != "" {
				return value.SessionID
			}
		case *deleteSessionParams:
			if value.SessionID != "" {
				return value.SessionID
			}
		}
	}
	return ""
}

func resolveSessionIDArgs(request Request) (sessionIDArgs, error) {
	sessionID := resolveSessionID(request)
	if strings.TrimSpace(sessionID) == "" {
		return sessionIDArgs{}, errors.New("session_id is required")
	}
	return sessionIDArgs{SessionID: sessionID}, nil
}

func resolveListSessionsArgs(request Request) string {
	workingDir := request.WorkingDir
	if len(request.Params) > 0 {
		var params listSessionsParams
		if err := json.Unmarshal(request.Params, &params); err == nil && workingDir == "" {
			workingDir = params.WorkingDir
		}
	}
	return workingDir
}

func mustWriteResult(response *Response, value any) {
	response.Result, _ = json.Marshal(value)
}
