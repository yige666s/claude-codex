package permissions

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/public/apperrors"
)

type Mode string

const (
	ModeDefault Mode = "default"
	ModePlan    Mode = "plan"
	ModeBypass  Mode = "bypass"
	ModeAuto    Mode = "auto"
)

type Level string

const (
	LevelNone    Level = "none"
	LevelRead    Level = "read"
	LevelWrite   Level = "write"
	LevelExecute Level = "execute"
)

func ParseMode(value string) (Mode, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", string(ModeDefault):
		return ModeDefault, nil
	case string(ModePlan):
		return ModePlan, nil
	case string(ModeBypass):
		return ModeBypass, nil
	case string(ModeAuto):
		return ModeAuto, nil
	default:
		return "", apperrors.Config(
			fmt.Sprintf("Unknown permission mode %q.", value),
			"Use one of: default, plan, bypass, auto.",
			nil,
		)
	}
}

type Checker struct {
	Mode     Mode
	input    io.Reader
	output   io.Writer
	requests RequestHandler
	// Cache of approved tool+level combinations for this session
	approved map[string]bool
}

type Request struct {
	ToolName string
	Level    Level
}

type RequestHandler func(ctx context.Context, request Request) error

type Option func(*Checker)

func WithRequestHandler(handler RequestHandler) Option {
	return func(checker *Checker) {
		checker.requests = handler
	}
}

func NewChecker(mode Mode, input io.Reader, output io.Writer, options ...Option) *Checker {
	checker := &Checker{
		Mode:     mode,
		input:    input,
		output:   output,
		approved: make(map[string]bool),
	}
	for _, option := range options {
		option(checker)
	}
	return checker
}

func (c *Checker) Authorize(ctx context.Context, toolName string, level Level) error {
	switch c.Mode {
	case ModeBypass:
		return nil
	case ModePlan:
		if level == LevelNone || level == LevelRead {
			return nil
		}
		return apperrors.Permission(
			fmt.Sprintf("Tool %s is blocked in plan mode.", toolName),
			"Switch to a write-capable permission mode before retrying.",
			nil,
		)
	case ModeAuto:
		if level == LevelNone || level == LevelRead {
			return nil
		}
		return apperrors.Permission(
			fmt.Sprintf("Tool %s is blocked in auto mode until rules are implemented.", toolName),
			"Use bypass or default mode for write and execute actions.",
			nil,
		)
	default:
		if level == LevelNone || level == LevelRead {
			return nil
		}

		// Check if this tool+level combination was already approved
		cacheKey := fmt.Sprintf("%s:%s", toolName, level)
		if c.approved[cacheKey] {
			return nil
		}

		var err error
		if c.requests != nil {
			err = c.requests(ctx, Request{
				ToolName: toolName,
				Level:    level,
			})
		} else {
			err = c.prompt(toolName, level)
		}

		// Cache the approval if successful
		if err == nil {
			c.approved[cacheKey] = true
		}

		return err
	}
}

func (c *Checker) prompt(toolName string, level Level) error {
	if c.input == nil || c.output == nil {
		return apperrors.Permission(
			fmt.Sprintf("Tool %s needs %s permission but no approval channel is available.", toolName, level),
			"Run with --permission-mode bypass for non-interactive execution, or attach stdin/stderr for prompts.",
			nil,
		)
	}

	if _, err := fmt.Fprintf(c.output, "Allow tool %s with %s permission? [y/N]: ", toolName, level); err != nil {
		return err
	}

	reader := bufio.NewReader(c.input)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return apperrors.Permission(
			fmt.Sprintf("Permission request for tool %s was interrupted.", toolName),
			"Retry the command and answer the prompt, or use --permission-mode bypass.",
			err,
		)
	}

	switch strings.TrimSpace(strings.ToLower(answer)) {
	case "y", "yes":
		return nil
	default:
		return apperrors.Permission(
			fmt.Sprintf("Tool %s was denied by the operator.", toolName),
			"Retry and approve the prompt if the action is expected.",
			nil,
		)
	}
}
