package cron

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCronCreateListDeleteLifecycle(t *testing.T) {
	ResetForTest()
	create := NewCronCreate()

	result, err := create.Execute(context.Background(), json.RawMessage(`{"cron":"7 * * * *","prompt":"check status","recurring":false,"durable":true}`))
	if err != nil {
		t.Fatalf("CronCreate Execute() error = %v", err)
	}
	var created CreateOutput
	if err := json.Unmarshal([]byte(result.Output), &created); err != nil {
		t.Fatalf("unmarshal create output %q: %v", result.Output, err)
	}
	if created.ID == "" || created.Cron != "7 * * * *" || created.HumanSchedule == "" || created.Recurring || !created.Durable {
		t.Fatalf("unexpected create output: %+v", created)
	}

	listResult, err := NewCronList().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CronList Execute() error = %v", err)
	}
	var listed ListOutput
	if err := json.Unmarshal([]byte(listResult.Output), &listed); err != nil {
		t.Fatalf("unmarshal list output %q: %v", listResult.Output, err)
	}
	if len(listed.Jobs) != 1 || listed.Jobs[0].ID != created.ID || listed.Jobs[0].Prompt != "check status" {
		t.Fatalf("unexpected list output: %+v", listed)
	}

	deletePayload, _ := json.Marshal(map[string]string{"id": created.ID})
	deleteResult, err := NewCronDelete().Execute(context.Background(), deletePayload)
	if err != nil {
		t.Fatalf("CronDelete Execute() error = %v", err)
	}
	var deleted DeleteOutput
	if err := json.Unmarshal([]byte(deleteResult.Output), &deleted); err != nil {
		t.Fatalf("unmarshal delete output %q: %v", deleteResult.Output, err)
	}
	if deleted.ID != created.ID {
		t.Fatalf("unexpected delete output: %+v", deleted)
	}

	listResult, err = NewCronList().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CronList after delete error = %v", err)
	}
	if err := json.Unmarshal([]byte(listResult.Output), &listed); err != nil {
		t.Fatalf("unmarshal empty list output %q: %v", listResult.Output, err)
	}
	if len(listed.Jobs) != 0 {
		t.Fatalf("expected empty job list, got %+v", listed)
	}
}

func TestCronCreateValidatesInput(t *testing.T) {
	ResetForTest()
	tool := NewCronCreate()

	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "missing cron", payload: `{"prompt":"x"}`, want: "cron expression is required"},
		{name: "missing prompt", payload: `{"cron":"* * * * *"}`, want: "prompt is required"},
		{name: "bad cron", payload: `{"cron":"* * *","prompt":"x"}`, want: "expected 5 fields"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), json.RawMessage(tc.payload))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCronCreateEnforcesMaxJobs(t *testing.T) {
	ResetForTest()
	tool := NewCronCreate()
	for i := 0; i < maxJobs; i++ {
		if _, err := tool.Execute(context.Background(), json.RawMessage(`{"cron":"*/5 * * * *","prompt":"x"}`)); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"cron":"*/5 * * * *","prompt":"overflow"}`))
	if err == nil || !strings.Contains(err.Error(), "Too many scheduled jobs") {
		t.Fatalf("expected max jobs error, got %v", err)
	}
}
