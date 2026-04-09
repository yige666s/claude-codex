package memdir

import (
	"fmt"
	"strings"
)

// CheckTeamMemSecrets blocks secrets from being written into shared team
// memory files. Non-team-memory paths are ignored.
func CheckTeamMemSecrets(filePath, content, projectRoot string) error {
	if !IsTeamMemPath(filePath, projectRoot) {
		return nil
	}

	matches := ScanForSecrets(content)
	if len(matches) == 0 {
		return nil
	}

	labels := make([]string, 0, len(matches))
	for _, match := range matches {
		labels = append(labels, match.Label)
	}

	return fmt.Errorf(
		"content contains potential secrets (%s) and cannot be written to team memory. Team memory is shared with all repository collaborators. Remove the sensitive content and try again",
		strings.Join(labels, ", "),
	)
}
