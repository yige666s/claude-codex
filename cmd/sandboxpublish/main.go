package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxtransfer"
)

func main() {
	root := flag.String("root", "", "absolute parent of skill directories")
	id := flag.String("id", "", "skill directory name")
	store := flag.String("store", "", "private content-addressed artifact store")
	flag.Parse()
	if !filepath.IsAbs(*root) || !filepath.IsAbs(*store) || *id == "" {
		fmt.Fprintln(os.Stderr, "absolute root/store and skill ID required")
		os.Exit(2)
	}
	builder := sandboxtransfer.Builder{SkillRoots: []string{*root}, Limits: sandboxtransfer.DevLimits()}
	ref, err := (sandboxartifact.Store{Dir: *store, Builder: builder}).Publish(context.Background(), *id, filepath.Join(*root, *id))
	if err != nil {
		fmt.Fprintln(os.Stderr, "publish rejected:", err)
		os.Exit(1)
	}
	fmt.Printf("%s@%s\n", ref.ID, ref.Digest)
}
