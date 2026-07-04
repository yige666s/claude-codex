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
		"<skill-runtime-instruction>You are already executing this skill. Follow the skill instructions using the tools available in this run; do not call another Skill tool to invoke this same skill. If the skill produces an artifact, create the final deliverable with Bash as needed and register only the final deliverable with the Artifact tool.</skill-runtime-instruction>",
	)
	if strings.TrimSpace(args) != "" {
		metadata = append(metadata, fmt.Sprintf("<command-args>%s</command-args>", args))
	}
	metadata = append(metadata, "", promptText)
	return strings.Join(metadata, "\n")
}
