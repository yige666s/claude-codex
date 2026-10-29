package sandboxbroker

import (
	"context"
	"errors"

	"claude-codex/internal/backend/agentruntime/sandboxstaging"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// StagingTransport is the only concrete Node transport at this stage. It can
// prepare/dispose signed input but cannot execute commands; no developer
// option may silently fall back to the legacy single-shot runner.
type StagingTransport struct{}

func (StagingTransport) PrepareIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) (string, error) {
	client, err := sandboxstaging.NewClient(endpoint, []byte(token), nil)
	if err != nil {
		return "", err
	}
	receipt, err := client.Prepare(ctx, sandboxstaging.StageRequest{
		TaskID: env.TaskID, UserID: env.UserID, SessionID: env.SessionID,
		PolicyRevision: env.PolicyRevision, Profile: env.Profile, SkillRef: env.SkillRef,
		Deadline: env.Deadline, Snapshot: env.Snapshot,
	})
	if err != nil {
		return "", err
	}
	return receipt.LeaseID, nil
}
func (StagingTransport) ExecuteIsolated(context.Context, string, string, ExecutionEnvelope) (string, error) {
	return "", errors.New("production command transport disabled; development execution handler is test-only")
}
func (StagingTransport) DisposeIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) error {
	client, err := sandboxstaging.NewClient(endpoint, []byte(token), nil)
	if err != nil {
		return err
	}
	return client.Dispose(ctx, env.LeaseID, env.UserID, false)
}

// DevelopmentNodeClient only targets a configured development pool reached
// over HTTPS or a loopback SSH tunnel. It is never selected by default.
type DevelopmentNodeClient struct{}

func (DevelopmentNodeClient) PrepareIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) (string, error) {
	return StagingTransport{}.PrepareIsolated(ctx, endpoint, token, env)
}
func (DevelopmentNodeClient) ExecuteIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) (string, error) {
	client, err := sandboxstaging.NewClient(endpoint, []byte(token), nil)
	if err != nil {
		return "", err
	}
	result, err := client.Execute(ctx, sandboxstaging.ExecuteRequest{StageControl: sandboxstaging.StageControl{LeaseID: env.LeaseID, UserID: env.UserID}, ExecutionID: env.ExecutionID, Script: []byte(env.Command)})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New("remote command failed")
	}
	return result.Stdout + result.Stderr, nil
}
func (DevelopmentNodeClient) CollectIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) ([]sandboxtransfer.OutputFile, error) {
	client, err := sandboxstaging.NewClient(endpoint, []byte(token), nil)
	if err != nil {
		return nil, err
	}
	return client.Collect(ctx, sandboxstaging.StageControl{LeaseID: env.LeaseID, UserID: env.UserID})
}
func (DevelopmentNodeClient) DisposeIsolated(ctx context.Context, endpoint, token string, env ExecutionEnvelope) error {
	return StagingTransport{}.DisposeIsolated(ctx, endpoint, token, env)
}
func (StagingTransport) CollectIsolated(context.Context, string, string, ExecutionEnvelope) ([]sandboxtransfer.OutputFile, error) {
	return nil, errors.New("production output collection disabled")
}
