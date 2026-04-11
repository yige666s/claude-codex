package main

import (
	"fmt"
	"os"

	"claude-codex/internal/app/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, cli.FormatError(err))
		os.Exit(1)
	}
}
