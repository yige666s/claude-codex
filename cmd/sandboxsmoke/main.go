package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxstaging"
	"claude-codex/internal/backend/sandboxtransfer"
)

func main() {
	config := flag.String("config", "", "Node development config")
	addr := flag.String("endpoint", "http://127.0.0.1:18080", "loopback endpoint")
	work := flag.String("workspace", "", "fake workspace directory")
	storeDir := flag.String("store", "", "private test artifact store")
	skillID := flag.String("skill", "smoke", "published skill ID to exercise")
	userID := flag.String("user", "smoke-user", "fake tenant identity")
	expected := flag.String("expected", "fake input", "expected fake input marker")
	quotaTest := flag.Bool("quota-test", false, "only fake tmpfs exhaustion test")
	flag.Parse()
	if err := smoke(*config, *addr, *work, *storeDir, *skillID, *userID, *expected, *quotaTest); err != nil {
		fmt.Fprintln(os.Stderr, "sandbox smoke failed:", err)
		os.Exit(1)
	}
	fmt.Println("sandbox smoke passed: prepare/execute/execute/collect/dispose, fake data only")
}
func smoke(config, endpoint, workspace, storeDir, skillID, userID, expected string, quotaTest bool) error {
	if !filepath.IsAbs(config) || !filepath.IsAbs(workspace) || !filepath.IsAbs(storeDir) {
		return errors.New("absolute paths required")
	}
	var cfg struct {
		Revision    string
		Profile     sandboxpolicy.Profile
		NodeKeyFile string
	}
	raw, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	key, err := os.ReadFile(cfg.NodeKeyFile)
	if err != nil {
		return err
	}
	ref, err := (sandboxartifact.Store{Dir: storeDir, Builder: sandboxtransfer.Builder{Limits: sandboxtransfer.DevLimits()}}).Resolve(skillID)
	if err != nil {
		return err
	}
	root := filepath.Dir(workspace)
	snapshot, err := (sandboxtransfer.Builder{WorkspaceRoot: root, Limits: sandboxtransfer.DevLimits()}).BuildWorkspace(context.Background(), workspace)
	if err != nil {
		return err
	}
	client, err := sandboxstaging.NewClient(endpoint, key, nil)
	if err != nil {
		return err
	}
	ctx, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	req := sandboxstaging.StageRequest{TaskID: "smoke-" + hex.EncodeToString(id[:]), UserID: userID, SessionID: "smoke-session", PolicyRevision: cfg.Revision, Profile: cfg.Profile, SkillRef: ref, Deadline: time.Now().Add(55 * time.Second).UTC(), Snapshot: snapshot}
	lease, err := client.Prepare(ctx, req)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = client.Dispose(cleanupCtx, lease.LeaseID, req.UserID, true)
	}()
	control := sandboxstaging.StageControl{LeaseID: lease.LeaseID, UserID: req.UserID}
	if _, err := client.Execute(ctx, sandboxstaging.ExecuteRequest{StageControl: sandboxstaging.StageControl{LeaseID: lease.LeaseID, UserID: userID + "-other"}, ExecutionID: hex.EncodeToString(id[:]), Script: []byte("echo forbidden")}); err == nil {
		return errors.New("cross-user command was accepted")
	}
	if err := client.Dispose(ctx, lease.LeaseID, userID+"-other", true); err == nil {
		return errors.New("cross-user cancellation was accepted")
	}
	other := req
	other.TaskID = "other-" + hex.EncodeToString(id[:])
	other.UserID = userID + "-other"
	if _, err := client.Prepare(ctx, other); err == nil {
		return errors.New("second tenant bypassed single-slot admission")
	}
	if quotaTest {
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		_, execErr := client.Execute(ctx, sandboxstaging.ExecuteRequest{StageControl: control, ExecutionID: hex.EncodeToString(id[:]), Script: []byte("dd if=/dev/zero of=/workspace/outputs/over-quota bs=1M count=80 2>/dev/null")})
		if execErr == nil {
			return errors.New("tmpfs quota did not stop large write")
		}
		if err := client.Dispose(ctx, lease.LeaseID, req.UserID, true); err != nil {
			return fmt.Errorf("quota cleanup failed: %w", err)
		}
		return nil
	}
	scripts := []string{
		"test -f /skill/SKILL.md && test ! -w /skill/SKILL.md && test ! -w /input/input.txt && test ! -e /var/run/docker.sock && test ! -e /etc/claude-sandbox-node/node.key && test ! -e /workspace/outputs/result.txt && command -v wget >/dev/null && if wget -q -T 1 -O /dev/null http://169.254.169.254/latest/meta-data/ >/dev/null 2>&1; then exit 42; fi; cat /workspace/input.txt && printf first > /workspace/outputs/result.txt",
		"printf second >> /workspace/outputs/result.txt",
	}
	for _, script := range scripts {
		if _, err = rand.Read(id[:]); err != nil {
			return err
		}
		result, execErr := client.Execute(ctx, sandboxstaging.ExecuteRequest{StageControl: control, ExecutionID: hex.EncodeToString(id[:]), Script: []byte(script)})
		if execErr != nil || result.ExitCode != 0 {
			return fmt.Errorf("execute rejected: %v", execErr)
		}
		if strings.Contains(script, "cat /workspace/input.txt") && !strings.Contains(result.Stdout, expected) {
			return errors.New("tenant input marker mismatch")
		}

	}
	files, err := client.Collect(ctx, control)
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}
	if len(files) != 1 || files[0].Path != "result.txt" || string(files[0].Content) != "firstsecond" {
		return errors.New("unexpected fake output")
	}
	if err := client.Dispose(ctx, lease.LeaseID, req.UserID, false); err != nil {
		return fmt.Errorf("dispose: %w", err)
	}
	return nil
}
