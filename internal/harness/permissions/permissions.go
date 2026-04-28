package permissions

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"claude-codex/internal/public/apperrors"
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

// PermissionModeFromString returns a mode or default for unknown values.
func PermissionModeFromString(value string) Mode {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", string(ModeDefault):
		return ModeDefault
	case string(ModePlan):
		return ModePlan
	case string(ModeBypass), "bypasspermissions":
		return ModeBypass
	case string(ModeAuto):
		return ModeAuto
	default:
		return ModeDefault
	}
}

// ToExternalPermissionMode maps internal modes to TS-compatible external names.
func ToExternalPermissionMode(mode Mode) string {
	switch mode {
	case ModeBypass:
		return "bypassPermissions"
	case ModePlan:
		return string(ModePlan)
	case ModeAuto:
		return string(ModeDefault)
	default:
		return string(ModeDefault)
	}
}

func IsDefaultMode(mode Mode) bool {
	return mode == "" || mode == ModeDefault
}

func PermissionModeTitle(mode Mode) string {
	switch mode {
	case ModePlan:
		return "Plan Mode"
	case ModeBypass:
		return "Bypass Permissions"
	case ModeAuto:
		return "Auto mode"
	default:
		return "Default"
	}
}

func PermissionModeShortTitle(mode Mode) string {
	switch mode {
	case ModePlan:
		return "Plan"
	case ModeBypass:
		return "Bypass"
	case ModeAuto:
		return "Auto"
	default:
		return "Default"
	}
}

type Checker struct {
	Mode        Mode
	input       io.Reader
	output      io.Writer
	requests    RequestHandler
	decisions   DecisionHandler
	resolvers   []DecisionResolver
	toolContext *ToolContext
	classifier  Classifier
	persist     UpdatePersister
	// Cache of approved tool+level combinations for this session
	approved map[string]bool
}

type Request struct {
	ToolName    string
	Level       Level
	Summary     string
	Metadata    map[string]string
	Suggestions []PermissionUpdate
}

type RequestHandler func(ctx context.Context, request Request) error

type UpdatePersister func(ctx context.Context, updates []PermissionUpdate) error

type Option func(*Checker)

func WithRequestHandler(handler RequestHandler) Option {
	return func(checker *Checker) {
		checker.requests = handler
	}
}

func WithDecisionHandler(handler DecisionHandler) Option {
	return func(checker *Checker) {
		checker.decisions = handler
	}
}

func WithDecisionResolver(resolver DecisionResolver) Option {
	return func(checker *Checker) {
		if resolver != nil {
			checker.resolvers = append(checker.resolvers, resolver)
		}
	}
}

func WithDecisionResolvers(resolvers ...DecisionResolver) Option {
	return func(checker *Checker) {
		for _, resolver := range resolvers {
			if resolver != nil {
				checker.resolvers = append(checker.resolvers, resolver)
			}
		}
	}
}

func WithToolContext(ctx *ToolContext) Option {
	return func(checker *Checker) {
		checker.toolContext = ctx
	}
}

func WithClassifier(classifier Classifier) Option {
	return func(checker *Checker) {
		checker.classifier = classifier
	}
}

func WithUpdatePersister(persist UpdatePersister) Option {
	return func(checker *Checker) {
		checker.persist = persist
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
	return c.AuthorizeRequest(ctx, Request{
		ToolName: toolName,
		Level:    level,
	})
}

func (c *Checker) AuthorizeRequest(ctx context.Context, request Request) error {
	if request.Metadata == nil {
		request.Metadata = map[string]string{}
	}
	if decision, ok := evaluateRuleDecision(c.toolContext, request); ok {
		return c.applyPermissionResult(ctx, request, decision)
	}

	switch c.Mode {
	case ModeBypass:
		return nil
	case ModePlan:
		if request.Level == LevelNone || request.Level == LevelRead {
			return nil
		}
		return apperrors.Permission(
			fmt.Sprintf("Tool %s is blocked in plan mode.", request.ToolName),
			"Switch to a write-capable permission mode before retrying.",
			nil,
		)
	case ModeAuto:
		if request.Level == LevelNone || request.Level == LevelRead {
			return nil
		}
		if IsAutoModeAllowlistedTool(request.ToolName) {
			return nil
		}
		if c.classifier == nil {
			return apperrors.Permission(
				fmt.Sprintf("Tool %s is blocked in auto mode until a classifier is configured.", request.ToolName),
				"Use bypass or default mode for write and execute actions, or configure an auto-mode classifier.",
				nil,
			)
		}
		result, err := c.classifier.ClassifyPermission(ctx, ClassifierRequest{
			ToolName: request.ToolName,
			Level:    request.Level,
			Summary:  request.Summary,
			Metadata: request.Metadata,
			Mode:     c.Mode,
		})
		if err != nil {
			return err
		}
		return c.applyClassifierResult(ctx, request, result)
	default:
		if request.Level == LevelNone || request.Level == LevelRead {
			return nil
		}

		// Check if this tool+level combination was already approved
		cacheKey := fmt.Sprintf("%s:%s", request.ToolName, request.Level)
		if c.approved[cacheKey] {
			return nil
		}

		return c.requestApproval(ctx, request)
	}
}

func (c *Checker) applyPermissionResult(ctx context.Context, request Request, result PermissionResult) error {
	switch result.Behavior {
	case BehaviorAllow:
		return nil
	case BehaviorDeny:
		message := result.Message
		if message == "" {
			message = fmt.Sprintf("Tool %s denied by permission rule.", request.ToolName)
		}
		return apperrors.Permission(message, "Adjust permission rules or choose a different action.", nil)
	case BehaviorAsk, BehaviorPassthrough:
		if len(request.Suggestions) == 0 {
			request.Suggestions = result.Suggestions
		}
		return c.requestApproval(ctx, request)
	default:
		return c.requestApproval(ctx, request)
	}
}

func (c *Checker) applyClassifierResult(ctx context.Context, request Request, result ClassifierResult) error {
	switch result.Behavior {
	case BehaviorAllow:
		return nil
	case BehaviorDeny:
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "classifier rejected this request"
		}
		classifier := strings.TrimSpace(result.Classifier)
		if classifier == "" {
			classifier = "auto-mode"
		}
		return apperrors.Permission(
			fmt.Sprintf("Tool %s was denied by %s classifier: %s", request.ToolName, classifier, reason),
			"Review the command or switch to default mode for an explicit approval prompt.",
			nil,
		)
	case BehaviorAsk, BehaviorPassthrough:
		if request.Suggestions == nil {
			request.Suggestions = approvalSuggestionsForRequest(request)
		}
		return c.requestApproval(ctx, request)
	default:
		return apperrors.Permission(
			fmt.Sprintf("Tool %s received an invalid classifier decision.", request.ToolName),
			"Retry in default mode or configure a valid classifier.",
			nil,
		)
	}
}

func (c *Checker) requestApproval(ctx context.Context, request Request) error {
	for _, resolver := range c.resolvers {
		decision, ok, err := resolver.ResolvePermission(ctx, request)
		if err != nil {
			return err
		}
		if ok {
			return c.applyDecision(ctx, request, decision)
		}
	}

	var decision Decision
	var err error
	switch {
	case c.decisions != nil:
		decision, err = c.decisions(ctx, request)
	case c.requests != nil:
		err = c.requests(ctx, request)
		if err == nil {
			decision = Decision{Behavior: BehaviorAllow, Remember: true}
		}
	default:
		decision, err = c.prompt(request)
	}
	if err != nil {
		return err
	}
	return c.applyDecision(ctx, request, decision)
}

func (c *Checker) applyDecision(ctx context.Context, request Request, decision Decision) error {
	if decision.Behavior == "" {
		decision.Behavior = BehaviorAllow
	}
	switch decision.Behavior {
	case BehaviorAllow:
		if c.toolContext != nil && len(decision.Updates) > 0 {
			updated := c.toolContext.ApplyUpdates(decision.Updates)
			*c.toolContext = *updated
		}
		if c.persist != nil && len(decision.Updates) > 0 {
			if err := c.persist(ctx, decision.Updates); err != nil {
				return err
			}
		}
		if decision.Remember {
			c.approved[fmt.Sprintf("%s:%s", request.ToolName, request.Level)] = true
		}
		return nil
	case BehaviorDeny:
		reason := decision.Reason
		if reason == "" {
			reason = fmt.Sprintf("Tool %s was denied by the operator.", request.ToolName)
		}
		return apperrors.Permission(reason, "Retry and approve the prompt if the action is expected.", nil)
	default:
		return apperrors.Permission(
			fmt.Sprintf("Tool %s did not receive an approval decision.", request.ToolName),
			"Retry and choose allow or deny.",
			nil,
		)
	}
}

func (c *Checker) prompt(request Request) (Decision, error) {
	if c.input == nil || c.output == nil {
		return Decision{}, apperrors.Permission(
			fmt.Sprintf("Tool %s needs %s permission but no approval channel is available.", request.ToolName, request.Level),
			"Run with --permission-mode bypass for non-interactive execution, or attach stdin/stderr for prompts.",
			nil,
		)
	}

	if _, err := fmt.Fprintf(c.output, "Allow tool %s with %s permission? [y/N]: ", request.ToolName, request.Level); err != nil {
		return Decision{}, err
	}

	reader := bufio.NewReader(c.input)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return Decision{}, apperrors.Permission(
			fmt.Sprintf("Permission request for tool %s was interrupted.", request.ToolName),
			"Retry the command and answer the prompt, or use --permission-mode bypass.",
			err,
		)
	}

	switch strings.TrimSpace(strings.ToLower(answer)) {
	case "y", "yes":
		return Decision{Behavior: BehaviorAllow}, nil
	default:
		return Decision{}, apperrors.Permission(
			fmt.Sprintf("Tool %s was denied by the operator.", request.ToolName),
			"Retry and approve the prompt if the action is expected.",
			nil,
		)
	}
}
