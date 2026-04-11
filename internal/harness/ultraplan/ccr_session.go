package ultraplan

import (
	"fmt"
	"strings"
)

const (
	ExitPlanModeToolName      = "exit_plan_mode"
	UltraplanTeleportSentinel = "__ULTRAPLAN_TELEPORT_LOCAL__"
)

type PollFailReason string

const (
	PollFailTerminated        PollFailReason = "terminated"
	PollFailTimeoutPending    PollFailReason = "timeout_pending"
	PollFailTimeoutNoPlan     PollFailReason = "timeout_no_plan"
	PollFailExtractMarkerMiss PollFailReason = "extract_marker_missing"
	PollFailNetworkOrUnknown  PollFailReason = "network_or_unknown"
	PollFailStopped           PollFailReason = "stopped"
)

type UltraplanPollError struct {
	Reason      PollFailReason
	RejectCount int
	Message     string
}

func (e *UltraplanPollError) Error() string { return e.Message }

type UltraplanPhase string

const (
	PhaseRunning    UltraplanPhase = "running"
	PhaseNeedsInput UltraplanPhase = "needs_input"
	PhasePlanReady  UltraplanPhase = "plan_ready"
)

type ScanResultKind string

const (
	ScanApproved   ScanResultKind = "approved"
	ScanTeleport   ScanResultKind = "teleport"
	ScanRejected   ScanResultKind = "rejected"
	ScanPending    ScanResultKind = "pending"
	ScanTerminated ScanResultKind = "terminated"
	ScanUnchanged  ScanResultKind = "unchanged"
)

type ToolUse struct {
	ID   string
	Name string
}

type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

type SDKMessage struct {
	Type        string
	SubType     string
	ToolUses    []ToolUse
	ToolResults []ToolResult
}

type ScanResult struct {
	Kind    ScanResultKind
	Plan    string
	ID      string
	Subtype string
}

type ExitPlanModeScanner struct {
	exitPlanCalls        []string
	results              map[string]ToolResult
	rejectedIDs          map[string]bool
	terminated           string
	rescanAfterRejection bool
	EverSeenPending      bool
}

func NewExitPlanModeScanner() *ExitPlanModeScanner {
	return &ExitPlanModeScanner{
		results:     map[string]ToolResult{},
		rejectedIDs: map[string]bool{},
	}
}

func (s *ExitPlanModeScanner) RejectCount() int {
	return len(s.rejectedIDs)
}

func (s *ExitPlanModeScanner) HasPendingPlan() bool {
	for i := len(s.exitPlanCalls) - 1; i >= 0; i-- {
		id := s.exitPlanCalls[i]
		if s.rejectedIDs[id] {
			continue
		}
		_, ok := s.results[id]
		return !ok
	}
	return false
}

func (s *ExitPlanModeScanner) Ingest(events []SDKMessage) (ScanResult, error) {
	for _, message := range events {
		switch message.Type {
		case "assistant":
			for _, toolUse := range message.ToolUses {
				if toolUse.Name == ExitPlanModeToolName {
					s.exitPlanCalls = append(s.exitPlanCalls, toolUse.ID)
				}
			}
		case "user":
			for _, result := range message.ToolResults {
				s.results[result.ToolUseID] = result
			}
		case "result":
			if message.SubType != "" && message.SubType != "success" {
				s.terminated = message.SubType
			}
		}
	}

	shouldScan := len(events) > 0 || s.rescanAfterRejection
	s.rescanAfterRejection = false
	if shouldScan {
		for i := len(s.exitPlanCalls) - 1; i >= 0; i-- {
			id := s.exitPlanCalls[i]
			if s.rejectedIDs[id] {
				continue
			}
			result, ok := s.results[id]
			if !ok {
				s.EverSeenPending = true
				return ScanResult{Kind: ScanPending}, nil
			}
			if result.IsError {
				plan := ExtractTeleportPlan(result.Content)
				if plan != "" {
					return ScanResult{Kind: ScanTeleport, Plan: plan}, nil
				}
				s.rejectedIDs[id] = true
				s.rescanAfterRejection = true
				return ScanResult{Kind: ScanRejected, ID: id}, nil
			}
			plan, err := ExtractApprovedPlan(result.Content)
			if err != nil {
				return ScanResult{}, err
			}
			return ScanResult{Kind: ScanApproved, Plan: plan}, nil
		}
	}
	if s.terminated != "" {
		return ScanResult{Kind: ScanTerminated, Subtype: s.terminated}, nil
	}
	return ScanResult{Kind: ScanUnchanged}, nil
}

func ExtractTeleportPlan(content string) string {
	marker := UltraplanTeleportSentinel + "\n"
	index := strings.Index(content, marker)
	if index == -1 {
		return ""
	}
	return strings.TrimRight(content[index+len(marker):], "\n")
}

func ExtractApprovedPlan(content string) (string, error) {
	for _, marker := range []string{
		"## Approved Plan (edited by user):\n",
		"## Approved Plan:\n",
	} {
		index := strings.Index(content, marker)
		if index != -1 {
			return strings.TrimRight(content[index+len(marker):], "\n"), nil
		}
	}
	return "", fmt.Errorf("approved plan marker missing")
}
