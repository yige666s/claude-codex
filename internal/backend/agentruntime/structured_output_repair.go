package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

const (
	structuredFallbackRepairRetry        = "repair_retry"
	structuredFallbackDeterministic      = "deterministic_fallback"
	structuredFallbackStopAutoExecute    = "stop_auto_execute"
	structuredFallbackRulePlanner        = "rule_planner_fallback"
	structuredFallbackUserClarification  = "user_clarification"
	structuredFallbackManualIntervention = "manual_intervention"
)

func repairStructuredJSONWithRunner(ctx context.Context, runner Runner, schema StructuredSchema, original string, failure error, contextText string) (json.RawMessage, error) {
	if runner == nil {
		err := fmt.Errorf("structured repair runner is not configured")
		emitStructuredOutputRepairEvent(ctx, schema, structuredOutputStatusFailed, err)
		return nil, err
	}
	prompt := structuredRepairPrompt(schema, original, failure, contextText)
	result, err := runner.RunGeneratedPrompt(ctx, state.NewSession(""), prompt)
	if err != nil {
		emitStructuredOutputRepairEvent(ctx, schema, structuredOutputStatusFailed, err)
		return nil, err
	}
	output := strings.TrimSpace(result.Output)
	if extracted := extractFirstJSONValue(output); extracted != "" {
		output = extracted
	}
	validation := ValidateStructuredJSON([]byte(output), schema)
	if !validation.Valid() {
		err := validation.Error()
		emitStructuredOutputRepairEvent(ctx, schema, structuredOutputStatusFailed, err)
		return nil, err
	}
	repaired, err := json.Marshal(validation.Value)
	if err != nil {
		emitStructuredOutputRepairEvent(ctx, schema, structuredOutputStatusFailed, err)
		return nil, err
	}
	emitStructuredOutputRepairEvent(ctx, schema, structuredOutputStatusSuccess, nil)
	return json.RawMessage(repaired), nil
}

func structuredRepairPrompt(schema StructuredSchema, original string, failure error, contextText string) string {
	schemaJSON, _ := json.MarshalIndent(schema.Schema, "", "  ")
	if strings.TrimSpace(contextText) == "" {
		contextText = "(none)"
	}
	return fmt.Sprintf(PromptStructuredJSONRepairTemplate, firstNonEmptyString(schema.Name, "structured_output"), firstNonEmptyString(schema.Version, "v1"), string(schemaJSON), strings.TrimSpace(fmt.Sprint(failure)), contextText, strings.TrimSpace(original))
}
