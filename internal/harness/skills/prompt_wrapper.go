package skills

import (
	"fmt"
	"strings"
)

func WrapGeneratedSkillPrompt(skillName, args, promptText string) string {
	var metadata []string
	metadata = append(metadata,
		fmt.Sprintf("<command-message>%s</command-message>", skillName),
		fmt.Sprintf("<command-name>/%s</command-name>", skillName),
		"<skill-format>true</skill-format>",
	)
	if strings.TrimSpace(args) != "" {
		metadata = append(metadata, fmt.Sprintf("<command-args>%s</command-args>", args))
	}
	metadata = append(metadata, "", promptText)
	return strings.Join(metadata, "\n")
}
