package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// Dry-run only: no network transport or archive persistence.
func main() {
	root := flag.String("root", "", "configured user workspace root")
	workspace := flag.String("workspace", "", "one user workspace")
	exclude := flag.String("exclude", "", "comma-separated top-level generated output directories")
	flag.Parse()
	if !filepath.IsAbs(*root) || !filepath.IsAbs(*workspace) {
		fmt.Fprintln(os.Stderr, "absolute root and workspace required")
		os.Exit(2)
	}
	var excluded []string
	if *exclude != "" {
		excluded = strings.Split(*exclude, ",")
	}
	builder := sandboxtransfer.Builder{WorkspaceRoot: *root, Limits: sandboxtransfer.DevLimits(), ExcludedWorkspaceDirs: excluded}
	archive, err := builder.BuildWorkspace(context.Background(), *workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snapshot rejected:", err)
		os.Exit(1)
	}
	sum := sha256.Sum256(archive)
	fmt.Printf("snapshot bytes=%d sha256=%s (not sent)\n", len(archive), hex.EncodeToString(sum[:]))
}
