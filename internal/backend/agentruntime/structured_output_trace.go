package agentruntime

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	structuredOutputValidationEvent = "structured_output_validation"
	structuredOutputRepairEvent     = "structured_output_repair"
	structuredOutputFallbackEvent   = "structured_output_fallback"

	structuredOutputStatusFailed  = "failed"
	structuredOutputStatusSuccess = "success"
)

type structuredOutputTraceEvent struct {
	SchemaName     string                      `json:"schema_name,omitempty"`
	SchemaVersion  string                      `json:"schema_version,omitempty"`
	Operation      string                      `json:"operation,omitempty"`
	Status         string                      `json:"status,omitempty"`
	FallbackLevel  string                      `json:"fallback_level,omitempty"`
	RepairAttempts int                         `json:"repair_attempts,omitempty"`
	Errors         []StructuredValidationError `json:"errors,omitempty"`
	Error          string                      `json:"error,omitempty"`
}

func emitStructuredOutputValidationFailure(ctx context.Context, schema StructuredSchema, operation string, result StructuredValidationResult) {
	if result.Valid() {
		return
	}
	emitStructuredOutputTraceEvent(ctx, structuredOutputValidationEvent, structuredOutputTraceEvent{
		SchemaName:    schema.Name,
		SchemaVersion: schema.Version,
		Operation:     operation,
		Status:        structuredOutputStatusFailed,
		Errors:        result.Errors,
		Error:         strings.TrimSpace(result.Error().Error()),
	})
}

func emitStructuredOutputRepairEvent(ctx context.Context, schema StructuredSchema, status string, err error) {
	record := structuredOutputTraceEvent{
		SchemaName:     schema.Name,
		SchemaVersion:  schema.Version,
		Operation:      "repair",
		Status:         status,
		RepairAttempts: 1,
	}
	if err != nil {
		record.Error = strings.TrimSpace(err.Error())
	}
	emitStructuredOutputTraceEvent(ctx, structuredOutputRepairEvent, record)
}

func emitStructuredOutputFallbackEvent(ctx context.Context, schema StructuredSchema, operation, fallbackLevel string, err error) {
	record := structuredOutputTraceEvent{
		SchemaName:    schema.Name,
		SchemaVersion: schema.Version,
		Operation:     operation,
		Status:        structuredOutputStatusSuccess,
		FallbackLevel: strings.TrimSpace(fallbackLevel),
	}
	if err != nil {
		record.Status = structuredOutputStatusFailed
		record.Error = strings.TrimSpace(err.Error())
	}
	emitStructuredOutputTraceEvent(ctx, structuredOutputFallbackEvent, record)
}

func emitStructuredOutputTraceEvent(ctx context.Context, eventType string, record structuredOutputTraceEvent) {
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	emitJobEventFromContext(ctx, Event{
		Type:    eventType,
		Role:    "system",
		Content: firstNonEmptyString(record.FallbackLevel, record.Status),
		Data:    data,
		Error:   record.Error,
	})
}
