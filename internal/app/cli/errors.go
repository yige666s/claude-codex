package cli

import (
	"errors"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/public/apperrors"
	"github.com/ding/claude-code/claude-go/internal/app/config"
)

func FormatError(err error) string {
	if err == nil {
		return ""
	}

	var loadErr *config.LoadError
	if errors.As(err, &loadErr) {
		return fmt.Sprintf("Config error: could not load %s\nHint: fix or remove the file and rerun.\nCause: %v", loadErr.Path, loadErr.Err)
	}

	return apperrors.FormatCLI(err)
}
