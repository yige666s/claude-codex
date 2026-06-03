package agentruntime

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type llmUsageScannerFunc func(dest ...any) error

func (f llmUsageScannerFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestScanLLMUsageRecordAllowsNullableTextFields(t *testing.T) {
	now := time.Now().UTC()
	values := []any{
		"usage-1",
		"alice",
		"session-1",
		nil,
		nil,
		"prompt",
		"v1",
		"hash",
		"experiment",
		"variant",
		"vertex",
		"gemini",
		10,
		20,
		30,
		0.001,
		1,
		"success",
		nil,
		int64(123),
		int64(45),
		now,
	}
	scanner := llmUsageScannerFunc(func(dest ...any) error {
		if len(dest) != len(values) {
			return fmt.Errorf("dest count = %d, want %d", len(dest), len(values))
		}
		for i, value := range values {
			switch out := dest[i].(type) {
			case *string:
				*out = value.(string)
			case *sql.NullString:
				if value == nil {
					*out = sql.NullString{}
				} else {
					*out = sql.NullString{String: value.(string), Valid: true}
				}
			case *int:
				*out = value.(int)
			case *int64:
				*out = value.(int64)
			case *float64:
				*out = value.(float64)
			case *any:
				*out = value
			default:
				return fmt.Errorf("unsupported scan dest %T at %d", dest[i], i)
			}
		}
		return nil
	})

	record, err := scanLLMUsageRecord(scanner)
	if err != nil {
		t.Fatalf("scanLLMUsageRecord() error = %v", err)
	}
	if record.RequestID != "" || record.SkillName != "" || record.Error != "" {
		t.Fatalf("nullable text fields = request %q skill %q error %q, want empty strings", record.RequestID, record.SkillName, record.Error)
	}
	if !record.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %v, want %v", record.CreatedAt, now)
	}
}
