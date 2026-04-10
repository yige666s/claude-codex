package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
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

type Request struct {
	ID         int64           `json:"id"`
	Method     string          `json:"method"`
	WorkingDir string          `json:"working_dir,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	Secret     string          `json:"secret,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
}

type runPromptParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
}

type listToolsParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
}

type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
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
	case "run_prompt":
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
		result, err := s.runner.RunPrompt(ctx, workingDir, prompt)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.Result, _ = json.Marshal(map[string]string{"output": result})
	case "list_tools":
		workingDir := request.WorkingDir
		if len(request.Params) > 0 {
			var params listToolsParams
			if err := json.Unmarshal(request.Params, &params); err == nil && workingDir == "" {
				workingDir = params.WorkingDir
			}
		}
		tools, err := s.runner.ListTools(ctx, workingDir)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.Result, _ = json.Marshal(map[string]any{"tools": tools})
	default:
		response.Error = fmt.Sprintf("unknown method %s", request.Method)
	}
	return response
}
