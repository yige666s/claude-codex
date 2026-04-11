package websandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

type ExecRequest struct {
	Name    string
	Args    []string
	Dir     string
	Env     map[string]string
	Timeout time.Duration
}

type ExecResult struct {
	Stdout string
	Stderr string
}

type CommandRunner interface {
	Run(ctx context.Context, request ExecRequest) (ExecResult, error)
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, request ExecRequest) (ExecResult, error) {
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, request.Name, request.Args...)
	if request.Dir != "" {
		cmd.Dir = request.Dir
	}
	cmd.Env = os.Environ()
	for key, value := range request.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, err
}
