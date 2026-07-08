package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestAdminOpsEvaluationRoutes(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	skills := NewMemorySkillExecutionStore()
	runtime.SetSkillExecutionStore(skills)
	usage := NewMemoryLLMUsageStore()
	risk := NewMemoryRiskStore()
	evaluations := NewMemoryEvaluationStore()

	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("build report")
	session.AddAssistantMessage("report ready")
	if err := runtime.sessions.Save(ctx, "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	succeeded, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "build report"}, "chat")
	if err != nil {
		t.Fatalf("create succeeded job: %v", err)
	}
	failed, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "broken report"}, "chat")
	if err != nil {
		t.Fatalf("create failed job: %v", err)
	}
	started := time.Now().UTC().Add(-2 * time.Second)
	finished := time.Now().UTC()
	for _, job := range []*Job{succeeded, failed} {
		if err := runtime.jobs.UpdateJobStatus(ctx, job.UserID, job.ID, JobStatusRunning, "", started); err != nil {
			t.Fatalf("mark running: %v", err)
		}
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, succeeded.UserID, succeeded.ID, JobStatusSucceeded, "", finished); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, failed.UserID, failed.ID, JobStatusFailed, "tool failed", finished); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := runtime.jobs.AddJobEvent(ctx, &JobEvent{
		ID:        NewJobEventID(),
		JobID:     failed.ID,
		UserID:    "alice",
		SessionID: session.ID,
		Type:      "error",
		Event:     Event{Type: "error", Role: state.MessageRoleTool, Error: "tool failed"},
		CreatedAt: finished,
	}); err != nil {
		t.Fatalf("add failed event: %v", err)
	}
	if err := skills.RecordSkillExecution(ctx, SkillExecutionRecord{
		SkillName:   "docx",
		UserID:      "alice",
		SessionID:   session.ID,
		JobID:       failed.ID,
		Status:      SkillExecutionStatusFailed,
		Error:       "tool failed",
		StartedAt:   started,
		CompletedAt: finished,
	}); err != nil {
		t.Fatalf("record skill execution: %v", err)
	}
	if err := usage.RecordLLMUsage(ctx, LLMUsageRecord{
		UserID:           "alice",
		SessionID:        session.ID,
		Provider:         "openai",
		Model:            "gpt-test",
		Status:           "success",
		InputTokens:      10,
		OutputTokens:     20,
		TotalTokens:      30,
		EstimatedCostUSD: 0.001,
		CreatedAt:        finished,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := risk.RecordRiskEvent(ctx, RiskEvent{
		UserID:    "alice",
		SessionID: session.ID,
		JobID:     failed.ID,
		Operation: RiskOperationJobCreate,
		Reason:    "job failed",
		RiskLevel: RiskLevelHigh,
		CreatedAt: finished,
	}); err != nil {
		t.Fatalf("record risk: %v", err)
	}

	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetLLMUsageStore(usage)
	server.SetRiskStore(risk)
	server.SetEvaluationStore(evaluations)

	body := `{"name":"real-data-eval","trigger":"manual","scope":{"subject_type":"job","user_id":"alice","session_id":"` + session.ID + `"}}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/runs", bytes.NewBufferString(body))
	createReq.Header.Set("X-User-ID", "admin")
	createReq.Header.Set("X-Admin-Token", "secret")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create eval status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Run     EvaluationRun        `json:"run"`
		Results []EvaluationResult   `json:"results"`
		Reviews []EvaluationReview   `json:"reviews"`
		Summary EvaluationRunSummary `json:"summary"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Run.Total != 2 || created.Run.Failed != 1 || created.Run.Passed != 1 {
		t.Fatalf("unexpected eval counters: %+v", created.Run)
	}
	if created.Run.ThresholdStatus != "" {
		t.Fatalf("threshold status = %q, want empty", created.Run.ThresholdStatus)
	}
	if len(created.Reviews) != 1 {
		t.Fatalf("review count = %d, want 1", len(created.Reviews))
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs", nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs/"+created.Run.ID, nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/results?run_id="+created.Run.ID+"&user_id=alice&status=failed", nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/summary", nil),
	} {
		req.Header.Set("X-User-ID", "admin")
		req.Header.Set("X-Admin-Token", "secret")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", req.URL.Path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), created.Run.ID) && req.URL.Path != "/v1/admin/ops/eval/summary" {
			t.Fatalf("%s missing run id in %s", req.URL.Path, rec.Body.String())
		}
	}

	csvReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/results?run_id="+created.Run.ID+"&status=failed&format=csv", nil)
	csvReq.Header.Set("X-User-ID", "admin")
	csvReq.Header.Set("X-Admin-Token", "secret")
	csvRec := httptest.NewRecorder()
	server.ServeHTTP(csvRec, csvReq)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("csv export status = %d body=%s", csvRec.Code, csvRec.Body.String())
	}
	if contentType := csvRec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("csv content type = %q", contentType)
	}
	if body := csvRec.Body.String(); !strings.Contains(body, "job_failed") || !strings.Contains(body, failed.ID) {
		t.Fatalf("csv export missing failed details: %s", body)
	}

	markdownReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/summary?format=markdown", nil)
	markdownReq.Header.Set("X-User-ID", "admin")
	markdownReq.Header.Set("X-Admin-Token", "secret")
	markdownRec := httptest.NewRecorder()
	server.ServeHTTP(markdownRec, markdownReq)
	if markdownRec.Code != http.StatusOK {
		t.Fatalf("markdown export status = %d body=%s", markdownRec.Code, markdownRec.Body.String())
	}
	if contentType := markdownRec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("markdown content type = %q", contentType)
	}
	if body := markdownRec.Body.String(); !strings.Contains(body, "# Agent Evaluation Summary") || !strings.Contains(body, created.Run.ID) {
		t.Fatalf("markdown export missing summary details: %s", body)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/v1/admin/ops/eval/reviews/"+created.Reviews[0].ID, bytes.NewBufferString(`{"status":"passed","note":"checked"}`))
	patchReq.Header.Set("X-User-ID", "admin")
	patchReq.Header.Set("X-Admin-Token", "secret")
	patchRec := httptest.NewRecorder()
	server.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK || !strings.Contains(patchRec.Body.String(), `"status":"passed"`) {
		t.Fatalf("patch review status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
}

func TestAdminOpsEvaluationRequiresAdminToken(t *testing.T) {
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs", nil)
	req.Header.Set("X-User-ID", "admin")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminOpsGoldenSetRoutesAndLLMJudgeRun(t *testing.T) {
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())
	server.SetEvaluationJudge(fixedGoldenJudge{result: GoldenJudgeResult{
		AnswerCorrectness: 0.92,
		AnswerRelevancy:   0.81,
		Faithfulness:      0.88,
		ContextPrecision:  0.76,
		ContextRecall:     0.79,
		Metadata:          map[string]any{"judge": "llm-as-judge", "model": "judge-model"},
	}})

	createBody := `{"id":"support-rag","name":"Support RAG","version":"v1","cases":[{"id":"case-1","query":"如何提高准确率？","expected_facts":["权限过滤"],"gold_evidence":[{"id":"doc-1","content":"需要权限过滤"}]}]}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-sets", bytes.NewBufferString(createBody))
	createReq.Header.Set("X-User-ID", "admin")
	createReq.Header.Set("X-Admin-Token", "secret")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create golden set status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if !strings.Contains(createRec.Body.String(), `"id":"support-rag"`) {
		t.Fatalf("create golden set missing id: %s", createRec.Body.String())
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/golden-sets", nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/golden-sets/support-rag", nil),
	} {
		req.Header.Set("X-User-ID", "admin")
		req.Header.Set("X-Admin-Token", "secret")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "support-rag") {
			t.Fatalf("%s status = %d body=%s", req.URL.Path, rec.Code, rec.Body.String())
		}
	}

	runBody := `{"set_id":"support-rag","judge":"llm","candidates":[{"case_id":"case-1","output":"回答需要权限过滤","retrieved_evidence":[{"id":"doc-1","content":"需要权限过滤"}]}]}`
	runReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-runs", bytes.NewBufferString(runBody))
	runReq.Header.Set("X-User-ID", "admin")
	runReq.Header.Set("X-Admin-Token", "secret")
	runRec := httptest.NewRecorder()
	server.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusCreated {
		t.Fatalf("create golden run status = %d body=%s", runRec.Code, runRec.Body.String())
	}
	if !strings.Contains(runRec.Body.String(), `"subject_type":"golden_case"`) || !strings.Contains(runRec.Body.String(), `"judge_model":"judge-model"`) {
		t.Fatalf("golden run response missing judge result: %s", runRec.Body.String())
	}
}

func TestAdminOpsRAGEvaluationRunImportsGoldSetAndAggregates(t *testing.T) {
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())

	body := `{
		"set_id":"black-wukong-rag",
		"set_version":"v1",
		"name":"Black Wukong RAG smoke",
		"knowledge_text":"黑神话悟空是一款以西游记为背景的动作角色扮演游戏。主角天命人会探索花果山、黑风山等区域，并与妖王战斗。游戏采用单机买断制。",
		"csv_content":"question,answer\n黑神话悟空是什么类型的游戏？,黑神话悟空是一款以西游记为背景的动作角色扮演游戏。\n主角被称为什么？,主角被称为天命人。",
		"judge":"heuristic",
		"chunk_size":40,
		"chunk_overlap":5,
		"top_k":3
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/rag-runs", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rag eval status = %d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, want := range []string{`"id":"black-wukong-rag"`, `"subject_type":"golden_case"`, `"answer_correctness_avg"`, `"context_recall_avg"`, `"chunk_count"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("rag eval response missing %s: %s", want, raw)
		}
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/golden-sets/black-wukong-rag?version=v1", nil)
	getReq.Header.Set("X-User-ID", "admin")
	getReq.Header.Set("X-Admin-Token", "secret")
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get imported golden set status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), `"csv_case_count":2`) {
		t.Fatalf("imported golden set missing metadata: %s", getRec.Body.String())
	}
}

func TestAdminOpsMemoryEvaluationRunUsesGoldenSetAndAggregates(t *testing.T) {
	root := t.TempDir()
	memory := NewFileMemoryService(root)
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root}, NewFileSessionStore(root), memory, nil, func(Scope) Runner { return echoRunner{} })
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())

	createBody := `{"id":"memory-smoke","name":"Memory Smoke","version":"v1","cases":[{"id":"case-style","query":"我汇报时喜欢什么格式？","expected_facts":["用户喜欢结构化输出"],"gold_evidence":[{"id":"mem-style","content":"用户喜欢结构化输出"}],"metadata":{"setup_memories":["用户喜欢结构化输出"],"expected_memory":"用户喜欢结构化输出","forbidden_memory":"用户喜欢散文式输出"}}]}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-sets", bytes.NewBufferString(createBody))
	createReq.Header.Set("X-User-ID", "admin")
	createReq.Header.Set("X-Admin-Token", "secret")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create memory golden set status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	runBody := `{"set_id":"memory-smoke","set_version":"v1","judge":"heuristic"}`
	runReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/memory-runs", bytes.NewBufferString(runBody))
	runReq.Header.Set("X-User-ID", "admin")
	runReq.Header.Set("X-Admin-Token", "secret")
	runRec := httptest.NewRecorder()
	server.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusCreated {
		t.Fatalf("create memory eval status = %d body=%s", runRec.Code, runRec.Body.String())
	}
	raw := runRec.Body.String()
	for _, want := range []string{`"subject_type":"golden_case"`, `"memory_context_recall_avg"`, `"namespace_isolation_pass_avg"`, `"answer_correctness_avg"`, `"source":"admin_memory_eval"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("memory eval response missing %s: %s", want, raw)
		}
	}
}

func TestAdminOpsGoldenCasesFromRuntimeTraceCreatesVersionedSet(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	store := NewMemoryEvaluationStore()
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(store)

	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("如何提升 RAG 准确率？")
	session.AddAssistantMessage("通过权限过滤和证据支撑提升 RAG 准确率。")
	if err := runtime.sessions.Save(ctx, "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	job, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "如何提升 RAG 准确率？"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	finished := time.Now().UTC()
	if err := runtime.jobs.UpdateJobStatus(ctx, job.UserID, job.ID, JobStatusSucceeded, "", finished); err != nil {
		t.Fatalf("finish job: %v", err)
	}

	body := `{"target_version":"v1","scope":{"subject_type":"job","user_id":"alice","session_id":"` + session.ID + `"},"subject_id":"` + job.ID + `","tags":["badcase"],"max_cases":1}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-sets/support-rag/cases/from-trace", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("capture status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"version":"v1"`) || !strings.Contains(rec.Body.String(), `"runtime_trace"`) {
		t.Fatalf("capture response missing versioned trace case: %s", rec.Body.String())
	}

	versionReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-sets/support-rag/versions", bytes.NewBufferString(`{"source_version":"v1","target_version":"v2","metadata":{"reason":"prompt-change"}}`))
	versionReq.Header.Set("X-User-ID", "admin")
	versionReq.Header.Set("X-Admin-Token", "secret")
	versionRec := httptest.NewRecorder()
	server.ServeHTTP(versionRec, versionReq)
	if versionRec.Code != http.StatusCreated {
		t.Fatalf("version status = %d body=%s", versionRec.Code, versionRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/golden-sets/support-rag?version=v1", nil)
	getReq.Header.Set("X-User-ID", "admin")
	getReq.Header.Set("X-Admin-Token", "secret")
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), `"version":"v1"`) {
		t.Fatalf("get v1 status = %d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestAdminOpsGoldenCasesFromEvaluationResultCreatesBadcase(t *testing.T) {
	ctx := context.Background()
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	store := NewMemoryEvaluationStore()
	server.SetEvaluationStore(store)
	run, err := store.CreateEvaluationRun(ctx, EvaluationRun{ID: "eval-run-1", Name: "prompt eval", Status: EvaluationRunStatusCompleted, Trigger: "test"})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	result, err := store.CreateEvaluationResult(ctx, EvaluationResult{
		RunID:         run.ID,
		SubjectType:   EvaluationSubjectGoldenCase,
		SubjectID:     "case-1",
		PromptID:      "runtime/deep_agent/planner",
		PromptVersion: "candidate",
		Status:        EvaluationResultStatusFailed,
		Score:         0.1,
		Input:         "Plan a research task",
		Output:        "Missing verification step",
		Findings:      []EvaluationFinding{{Severity: "error", Code: "missing_verification", Message: "Candidate omitted verification"}},
	})
	if err != nil {
		t.Fatalf("create result: %v", err)
	}
	body := `{"target_version":"v1","evaluation_result_id":"` + result.ID + `","tags":["phase5"],"max_cases":1}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/golden-sets/prompt-badcases/cases/from-trace", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("capture status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"source":"evaluation_result"`) || !strings.Contains(rec.Body.String(), `"badcase"`) || !strings.Contains(rec.Body.String(), `"missing_verification"`) {
		t.Fatalf("capture response missing eval-result badcase: %s", rec.Body.String())
	}
}

func TestAdminOpsTemplateReplayCorpusDashboardAndRoutes(t *testing.T) {
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())

	corpusReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/template-corpus", nil)
	corpusReq.Header.Set("X-User-ID", "admin")
	corpusReq.Header.Set("X-Admin-Token", "secret")
	corpusRec := httptest.NewRecorder()
	server.ServeHTTP(corpusRec, corpusReq)
	if corpusRec.Code != http.StatusOK {
		t.Fatalf("corpus status = %d body=%s", corpusRec.Code, corpusRec.Body.String())
	}
	if body := corpusRec.Body.String(); !strings.Contains(body, DeepAgentTemplateResearchReport) || !strings.Contains(body, DeepAgentTemplateCodeFix) || !strings.Contains(body, DeepAgentTemplateDocGeneration) {
		t.Fatalf("corpus response missing template sets: %s", body)
	}

	replayBody := `{"persist":true,"planner_version":"planner-v2","router_version":"router-v2","executor_version":"executor-v2","verifier_version":"verifier-v2","experiment_id":"phase11","variant_id":"candidate"}`
	replayReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/template-replay", bytes.NewBufferString(replayBody))
	replayReq.Header.Set("X-User-ID", "admin")
	replayReq.Header.Set("X-Admin-Token", "secret")
	replayRec := httptest.NewRecorder()
	server.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusCreated {
		t.Fatalf("replay status = %d body=%s", replayRec.Code, replayRec.Body.String())
	}
	var replay DeepAgentTemplateReplayReport
	if err := json.Unmarshal(replayRec.Body.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if len(replay.Runs) != 3 || replay.Dashboard.Total != 9 {
		t.Fatalf("unexpected replay shape: runs=%d dashboard=%+v", len(replay.Runs), replay.Dashboard)
	}
	for _, row := range replay.Dashboard.Templates {
		if row.Total != 3 || row.SuccessRate <= 0 || row.BlockedRate == 0 || row.EstimatedCostUSD == 0 || row.TokenEstimate == 0 || len(row.VerifierFailureReasons) == 0 {
			t.Fatalf("template row missing phase11 metrics: %+v", row)
		}
		if row.Versions["planner"] != "planner-v2" || row.Versions["router"] != "router-v2" || row.Versions["executor"] != "executor-v2" || row.Versions["verifier"] != "verifier-v2" {
			t.Fatalf("template row missing component versions: %+v", row)
		}
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/template-dashboard", nil)
	dashboardReq.Header.Set("X-User-ID", "admin")
	dashboardReq.Header.Set("X-Admin-Token", "secret")
	dashboardRec := httptest.NewRecorder()
	server.ServeHTTP(dashboardRec, dashboardReq)
	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%s", dashboardRec.Code, dashboardRec.Body.String())
	}
	if body := dashboardRec.Body.String(); !strings.Contains(body, `"total":9`) || !strings.Contains(body, `"blocked_rate"`) || !strings.Contains(body, `"estimated_cost_usd"`) {
		t.Fatalf("dashboard response missing metrics: %s", body)
	}
}
