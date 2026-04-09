package compact

const (
	// NoToolsPreamble instructs the model not to use tools during compaction
	NoToolsPreamble = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

`

	// DetailedAnalysisInstructionBase provides analysis instructions for full compaction
	DetailedAnalysisInstructionBase = `Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.`

	// DetailedAnalysisInstructionPartial provides analysis instructions for partial compaction
	DetailedAnalysisInstructionPartial = `Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Analyze the recent messages chronologically. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.`

	// BaseCompactPrompt is the main prompt for full conversation compaction
	BaseCompactPrompt = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

` + DetailedAnalysisInstructionBase + `

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirming with the user first.
                       If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were working on and where you left off. This should be verbatim to ensure there's no drift in task interpretation.

Here's an example of how your output should be structured:

<analysis>
[Your detailed chronological analysis here, working through each message and identifying key points]
</analysis>

<summary>
# Summary

## 1. Primary Request and Intent
[Detailed description of user's requests]

## 2. Key Technical Concepts
- [Concept 1]
- [Concept 2]

## 3. Files and Code Sections
- [File path]: [Description and code snippets]

## 4. Errors and Fixes
- [Error description and resolution]

## 5. Problem Solving
[Description of problems and solutions]

## 6. All User Messages
- [User message 1]
- [User message 2]

## 7. Pending Tasks
- [Task 1]
- [Task 2]

## 8. Current Work
[Detailed description with file names and code]

## 9. Optional Next Step
[Next step if applicable, with quotes from conversation]
</summary>`

	// PartialCompactPromptPrefix is the prompt for partial (recent messages) compaction
	PartialCompactPromptPrefix = `Your task is to create a detailed summary of the RECENT messages in this conversation, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions from the recent work.

` + DetailedAnalysisInstructionPartial + `

Your summary should include the following sections:

1. Recent Work: Describe what was being worked on in the recent messages
2. Key Technical Details: List important technical concepts, files, and code patterns from recent work
3. Errors and Fixes: Document any errors encountered and how they were resolved
4. User Feedback: Note any specific user feedback or direction changes
5. Current State: Describe the current state of the work

Here's an example of how your output should be structured:

<analysis>
[Your detailed analysis of recent messages]
</analysis>

<summary>
# Recent Messages Summary

## 1. Recent Work
[Description of recent work]

## 2. Key Technical Details
- [Detail 1]
- [Detail 2]

## 3. Errors and Fixes
- [Error and resolution]

## 4. User Feedback
- [Feedback item]

## 5. Current State
[Current state description]
</summary>`
)

// GetCompactPrompt returns the appropriate compaction prompt
func GetCompactPrompt(isPartial bool, customInstructions string) string {
	prompt := NoToolsPreamble

	if isPartial {
		prompt += PartialCompactPromptPrefix
	} else {
		prompt += BaseCompactPrompt
	}

	if customInstructions != "" {
		prompt += "\n\nAdditional instructions:\n" + customInstructions
	}

	return prompt
}

// FormatCompactSummary formats the compact summary by removing analysis tags
func FormatCompactSummary(summary string) string {
	// Remove <analysis> blocks
	result := summary

	// Simple regex-like removal of analysis blocks
	// In production, use proper XML/HTML parsing
	start := 0
	for {
		analysisStart := findString(result[start:], "<analysis>")
		if analysisStart == -1 {
			break
		}
		analysisStart += start

		analysisEnd := findString(result[analysisStart:], "</analysis>")
		if analysisEnd == -1 {
			break
		}
		analysisEnd += analysisStart + len("</analysis>")

		result = result[:analysisStart] + result[analysisEnd:]
		start = analysisStart
	}

	return result
}

// GetCompactUserSummaryMessage creates the user-facing summary message
func GetCompactUserSummaryMessage(
	summary string,
	suppressFollowUpQuestions bool,
	transcriptPath string,
	recentMessagesPreserved bool,
) string {
	formattedSummary := FormatCompactSummary(summary)

	baseSummary := "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n" + formattedSummary

	if transcriptPath != "" {
		baseSummary += "\n\nIf you need specific details from before compaction (like exact code snippets, error messages, or content you generated), read the full transcript at: " + transcriptPath
	}

	if recentMessagesPreserved {
		baseSummary += "\n\nRecent messages are preserved verbatim."
	}

	if suppressFollowUpQuestions {
		continuation := baseSummary + "\nContinue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, do not preface with \"I'll continue\" or similar. Pick up the last task as if the break never happened."
		return continuation
	}

	return baseSummary
}

// Helper function to find substring
func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
