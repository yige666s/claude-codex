package sandboxoci

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// Optional Linux Docker contract test. Run only on a dedicated dev daemon
// with a locally cached, digest-pinned image (e.g. BusyBox).
func TestDockerExecutorIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires a Linux Docker daemon")
	}
	image := os.Getenv("SANDBOX_OCI_TEST_IMAGE")
	if image == "" {
		t.Skip("set SANDBOX_OCI_TEST_IMAGE to a cached repo@sha256 digest")
	}
	profile := validProfile()
	profile.ImageDigest = image
	profile.TimeoutSeconds = 10
	profile.MaxOutputBytes = 1024
	executor := DockerExecutor{}
	script := []byte("set -eu\ntest \"$(id -u)\" = 65534\ntest ! -e /var/run/docker.sock\ntest ! -e /run/agentapi/secrets\nprintf x > /workspace/result\nif touch /etc/sandbox-writable 2>/dev/null; then exit 17; fi\nprintf p3-isolated-ok\n")
	result, err := executor.Execute(context.Background(), profile, script)
	if err != nil || result.Stdout != "p3-isolated-ok" {
		t.Fatalf("isolation result=%+v, err=%v", result, err)
	}
	profile.TimeoutSeconds = 1
	if _, err = executor.Execute(context.Background(), profile, []byte("sleep 5")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout cleanup err=%v", err)
	}
	profile.TimeoutSeconds = 10
	profile.MaxOutputBytes = 16
	limited, err := executor.Execute(context.Background(), profile, []byte("printf 123456789012345678901234567890"))
	if err == nil || len(limited.Stdout) > 16 {
		t.Fatalf("output limit result=%+v, err=%v", limited, err)
	}
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "label=agentapi.sandbox_task=true").Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("orphan sandbox containers=%q, err=%v", out, err)
	}
}
