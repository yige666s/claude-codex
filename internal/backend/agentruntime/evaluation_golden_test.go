package agentruntime

import (
	"context"
	"testing"
	"time"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

func TestEvaluationEngineEvaluatesGoldenSetWithRAGASStyleMetrics(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	engine := NewEvaluationEngine(staticEvaluationTraceSource{})
	engine.Now = func() time.Time { return now }

	report, err := engine.EvaluateGolden(context.Background(), GoldenEvaluationRequest{
		Name:    "support-rag-regression",
		Trigger: "manual",
		Set: GoldenSet{
			ID:      "support-rag",
			Name:    "Support RAG",
			Version: "v1",
			Cases: []GoldenCase{{
				ID:             "case-1",
				Query:          "企业级 SaaS 支持助手怎么提高准确率？",
				ExpectedAnswer: "使用 hybrid RAG、权限过滤和 groundedness 评测提高准确率。",
				ExpectedFacts: []string{
					"hybrid RAG",
					"权限过滤",
					"groundedness 评测",
				},
				GoldEvidence: []GoldenEvidence{{
					ID:      "doc-1",
					Content: "企业级 SaaS 支持助手通过 hybrid RAG、权限过滤和 groundedness 评测提升回答准确率。",
					Source:  "support-plan.md",
				}},
				Tags: []string{"rag", "support"},
			}},
		},
		Candidates: []GoldenCandidate{{
			CaseID: "case-1",
			Output: "可以使用 hybrid RAG 做语义和关键词融合，并结合权限过滤和 groundedness 评测来提升准确率。",
			RetrievedEvidence: []GoldenEvidence{{
				ID:      "doc-1",
				Content: "企业级 SaaS 支持助手通过 hybrid RAG、权限过滤和 groundedness 评测提升回答准确率。",
				Source:  "support-plan.md",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("EvaluateGolden returned error: %v", err)
	}
	if report.Run.Status != EvaluationRunStatusCompleted {
		t.Fatalf("run status = %q, want completed", report.Run.Status)
	}
	if report.Run.Total != 1 || report.Run.Passed != 1 || report.Run.Failed != 0 || report.Run.Warning != 0 {
		t.Fatalf("unexpected counters: total=%d passed=%d failed=%d warning=%d findings=%v", report.Run.Total, report.Run.Passed, report.Run.Failed, report.Run.Warning, report.Results[0].Findings)
	}
	if len(report.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(report.Results))
	}
	result := report.Results[0]
	if result.SubjectType != EvaluationSubjectGoldenCase {
		t.Fatalf("subject type = %q, want golden case", result.SubjectType)
	}
	for _, key := range []string{
		EvaluationMetricAnswerCorrectness,
		EvaluationMetricAnswerRelevancy,
		EvaluationMetricFaithfulness,
		EvaluationMetricContextPrecision,
		EvaluationMetricContextRecall,
	} {
		if _, ok := result.Metrics[key]; !ok {
			t.Fatalf("missing metric %q in %#v", key, result.Metrics)
		}
	}
	if got := mapFloat(report.Run.Metrics, EvaluationMetricAnswerCorrectness+"_avg"); got < 0.99 {
		t.Fatalf("answer correctness avg = %v, want near 1", got)
	}
	if len(report.Reviews) != 0 {
		t.Fatalf("review count = %d, want 0", len(report.Reviews))
	}
}

func TestEvaluationEngineGoldenSetCreatesReviewForMissingEvidence(t *testing.T) {
	engine := NewEvaluationEngine(staticEvaluationTraceSource{})
	report, err := engine.EvaluateGolden(context.Background(), GoldenEvaluationRequest{
		Set: GoldenSet{
			ID:   "support-rag",
			Name: "Support RAG",
			Cases: []GoldenCase{{
				ID:            "case-1",
				Query:         "如何提高回答准确率？",
				ExpectedFacts: []string{"权限过滤", "证据支撑"},
				GoldEvidence: []GoldenEvidence{{
					ID:      "doc-1",
					Content: "回答需要权限过滤，并且需要证据支撑。",
				}},
			}},
		},
		Candidates: []GoldenCandidate{{
			CaseID:            "case-1",
			Output:            "多调几次模型即可。",
			RetrievedEvidence: []GoldenEvidence{{ID: "doc-other", Content: "无关内容"}},
		}},
	})
	if err != nil {
		t.Fatalf("EvaluateGolden returned error: %v", err)
	}
	if report.Run.Failed != 1 || report.Results[0].Status != EvaluationResultStatusFailed {
		t.Fatalf("run failed=%d result=%s findings=%v", report.Run.Failed, report.Results[0].Status, report.Results[0].Findings)
	}
	if len(report.Reviews) != 1 || report.Reviews[0].Status != EvaluationReviewStatusPending {
		t.Fatalf("unexpected reviews: %#v", report.Reviews)
	}
	if got := mapFloat(report.Results[0].Metrics, EvaluationMetricContextRecall); got != 0 {
		t.Fatalf("context recall = %v, want 0", got)
	}
}

type fixedGoldenJudge struct {
	result GoldenJudgeResult
}

func (j fixedGoldenJudge) JudgeGoldenCase(context.Context, GoldenJudgeRequest) (GoldenJudgeResult, error) {
	return j.result, nil
}

func TestEvaluationEngineGoldenSetCanUseLLMJudgeAdapter(t *testing.T) {
	engine := NewEvaluationEngine(staticEvaluationTraceSource{})
	engine.Judge = fixedGoldenJudge{result: GoldenJudgeResult{
		AnswerCorrectness: 0.9,
		AnswerRelevancy:   0.8,
		Faithfulness:      0.85,
		ContextPrecision:  0.7,
		ContextRecall:     0.75,
		Metadata:          map[string]any{"judge": "llm-as-judge", "model": "judge-model"},
	}}
	report, err := engine.EvaluateGolden(context.Background(), GoldenEvaluationRequest{
		Set: GoldenSet{
			ID:    "judge-set",
			Cases: []GoldenCase{{ID: "case-1", Query: "问题", ExpectedFacts: []string{"事实"}}},
		},
		Candidates: []GoldenCandidate{{CaseID: "case-1", Output: "回答"}},
	})
	if err != nil {
		t.Fatalf("EvaluateGolden returned error: %v", err)
	}
	if report.Results[0].Status != EvaluationResultStatusPassed {
		t.Fatalf("status = %q, findings=%v", report.Results[0].Status, report.Results[0].Findings)
	}
	if got := report.Results[0].Metrics["judge_judge"]; got != "llm-as-judge" {
		t.Fatalf("judge metadata = %#v", report.Results[0].Metrics)
	}
	if got := report.Results[0].Metrics["judge_model"]; got != "judge-model" {
		t.Fatalf("judge model metadata = %#v", report.Results[0].Metrics)
	}
}

type jsonJudgePlanner struct {
	response string
}

func (p jsonJudgePlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	return plannerapi.Plan{AssistantText: p.response}, nil
}

func TestPlannerGoldenJudgeParsesLLMJudgeJSON(t *testing.T) {
	judge := PlannerGoldenJudge{
		Planner:       jsonJudgePlanner{response: `{"answer_correctness":0.91,"answer_relevancy":0.82,"faithfulness":0.73,"context_precision":0.64,"context_recall":0.55}`},
		Model:         "judge-model",
		PromptVersion: "ragas-json-v2",
	}
	result, err := judge.JudgeGoldenCase(context.Background(), GoldenJudgeRequest{
		Case:      GoldenCase{ID: "case-1", Query: "问题", ExpectedFacts: []string{"事实"}},
		Candidate: GoldenCandidate{CaseID: "case-1", Output: "回答"},
	})
	if err != nil {
		t.Fatalf("JudgeGoldenCase returned error: %v", err)
	}
	if result.AnswerCorrectness != 0.91 || result.ContextRecall != 0.55 {
		t.Fatalf("unexpected scores: %+v", result)
	}
	if result.Metadata["judge"] != "llm-as-judge" || result.Metadata["model"] != "judge-model" || result.Metadata["prompt_version"] != "ragas-json-v2" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

type sequenceJudgePlanner struct {
	responses []string
	calls     int
}

func (p *sequenceJudgePlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	if p.calls >= len(p.responses) {
		return plannerapi.Plan{}, nil
	}
	response := p.responses[p.calls]
	p.calls++
	return plannerapi.Plan{AssistantText: response}, nil
}

func TestPlannerGoldenJudgeRepairsNonJSONResponse(t *testing.T) {
	planner := &sequenceJudgePlanner{responses: []string{
		"Scores look good overall.",
		`{"answer_correctness":1,"answer_relevancy":0.8,"faithfulness":0.7,"context_precision":0.6,"context_recall":0.5}`,
	}}
	judge := PlannerGoldenJudge{Planner: planner}
	result, err := judge.JudgeGoldenCase(context.Background(), GoldenJudgeRequest{
		Case:      GoldenCase{ID: "case-1", Query: "问题", ExpectedFacts: []string{"事实"}},
		Candidate: GoldenCandidate{CaseID: "case-1", Output: "回答"},
	})
	if err != nil {
		t.Fatalf("JudgeGoldenCase returned error: %v", err)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", planner.calls)
	}
	if result.AnswerCorrectness != 1 || result.ContextRecall != 0.5 {
		t.Fatalf("unexpected scores: %+v", result)
	}
	if result.Metadata["judge"] != "llm-as-judge" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

func TestPlannerGoldenJudgeFallsBackWhenJSONRepairFails(t *testing.T) {
	planner := &sequenceJudgePlanner{responses: []string{"not json", "still not json"}}
	judge := PlannerGoldenJudge{Planner: planner, Model: "judge-model"}
	result, err := judge.JudgeGoldenCase(context.Background(), GoldenJudgeRequest{
		Case: GoldenCase{
			ID:            "case-1",
			Query:         "What is the codename?",
			ExpectedFacts: []string{"Aurora Smoke"},
			GoldEvidence:  []GoldenEvidence{{Content: "Aurora Smoke is the codename."}},
		},
		Candidate: GoldenCandidate{
			CaseID:            "case-1",
			Output:            "Aurora Smoke is the codename.",
			RetrievedEvidence: []GoldenEvidence{{Content: "Aurora Smoke is the codename."}},
		},
	})
	if err != nil {
		t.Fatalf("JudgeGoldenCase returned error: %v", err)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", planner.calls)
	}
	if result.Metadata["judge"] != "llm-as-judge-fallback" || result.Metadata["model"] != "judge-model" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	if result.AnswerCorrectness <= 0 {
		t.Fatalf("fallback did not score candidate: %+v", result)
	}
}
