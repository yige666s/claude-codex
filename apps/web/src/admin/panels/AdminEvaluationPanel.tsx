import { useEffect, useMemo, useState } from "react";
import { Activity, AlertCircle, Archive, Briefcase, Clock, Database, Download, FileText, FileUp, Info, MessageCircle, PlayCircle, Settings, RefreshCw, Search, ShieldCheck, Sparkles, Square, UserX, X } from "lucide-react";
import { ApiClient } from "../../api/client";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Textarea } from "../../components/ui/textarea";
import { AdminSectionNotice } from "../ui";
import {
  AdminMetric,
  AdminTabs,
  StatusBadge,
  SkillFact,
  auditRecordSummary,
  auditRiskForEventName,
  downloadTextFile,
  errorMessage,
  filterEvaluationResults,
  formatAuditMetadata,
  formatBytes,
  formatLatencyMetric,
  formatNumber,
  formatPercent,
  formatShortDate,
  formatTime,
  formatUSD,
  fuzzyMatch,
  initials,
  mergeEvaluationReviews,
  metricNumber,
  riskEventSummary,
  selectedRunPassRate,
  terminalJobs,
  type AdminTabOption
} from "../shared";
import { sessionTitle } from "../../lib/sessionTitle";
import type { AdminHealthStatus, AdminUser, Asset, AuditLogRecord, AuditLogSummary, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunSummary, GoldenCandidate, GoldenSet, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, RiskReviewSummary, RiskSummary, Session } from "../../types";

function splitGoldenLines(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function AdminEvaluationPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [runs, setRuns] = useState<EvaluationRun[]>([]);
  const [summary, setSummary] = useState<EvaluationRunSummary | null>(null);
  const [results, setResults] = useState<EvaluationResult[]>([]);
  const [reviews, setReviews] = useState<EvaluationReview[]>([]);
  const [selectedRunID, setSelectedRunID] = useState("");
  const [selectedResultID, setSelectedResultID] = useState("");
  const [userID, setUserID] = useState("");
  const [sessionID, setSessionID] = useState("");
  const [jobID, setJobID] = useState("");
  const [skillName, setSkillName] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [subjectType, setSubjectType] = useState("job");
  const [runStatusFilter, setRunStatusFilter] = useState("all");
  const [resultStatusFilter, setResultStatusFilter] = useState("all");
  const [days, setDays] = useState(7);
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [exportBusy, setExportBusy] = useState("");
  const [reviewBusy, setReviewBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [evaluationTab, setEvaluationTab] = useState<"results" | "selected" | "reviews" | "io" | "golden">("results");
  const [goldenSets, setGoldenSets] = useState<GoldenSet[]>([]);
  const [goldenSetID, setGoldenSetID] = useState("runtime-golden");
  const [goldenSourceVersion, setGoldenSourceVersion] = useState("");
  const [goldenTargetVersion, setGoldenTargetVersion] = useState("v1");
  const [goldenTraceSubjectType, setGoldenTraceSubjectType] = useState("job");
  const [goldenSubjectID, setGoldenSubjectID] = useState("");
  const [goldenMaxCases, setGoldenMaxCases] = useState(5);
  const [goldenTags, setGoldenTags] = useState("from-runtime");
  const [goldenExpectedAnswer, setGoldenExpectedAnswer] = useState("");
  const [goldenExpectedFacts, setGoldenExpectedFacts] = useState("");
  const [goldenJudge, setGoldenJudge] = useState("heuristic");
  const [goldenBusy, setGoldenBusy] = useState("");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedRun = runs.find((run) => run.id === selectedRunID) || runs[0] || null;
  const selectedResult = results.find((result) => result.id === selectedResultID) || results[0] || null;
  const selectedGoldenSet = useMemo(() => {
    const cleanID = goldenSetID.trim();
    const cleanVersion = goldenTargetVersion.trim() || goldenSourceVersion.trim();
    return goldenSets.find((set) => set.id === cleanID && (!cleanVersion || set.version === cleanVersion))
      || goldenSets.find((set) => set.id === cleanID)
      || goldenSets[0]
      || null;
  }, [goldenSetID, goldenSets, goldenSourceVersion, goldenTargetVersion]);
  const selectedGoldenSetKey = selectedGoldenSet ? `${selectedGoldenSet.id}::${selectedGoldenSet.version || ""}` : "";
  const reviewsByResultID = useMemo(() => {
    const map = new Map<string, EvaluationReview[]>();
    reviews.forEach((review) => {
      const list = map.get(review.result_id) || [];
      list.push(review);
      map.set(review.result_id, list);
    });
    return map;
  }, [reviews]);

  const loadEvaluation = async (runID = selectedRunID) => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const [summaryPayload, nextReviews] = await Promise.all([
        api.adminOpsEvaluationSummary(token, { from, status: runStatusFilter, limit: 500 }),
        api.adminOpsEvaluationReviews(token, { status: "all", limit: 500 })
      ]);
      setSummary(summaryPayload.summary);
      setRuns(summaryPayload.runs);
      setReviews(nextReviews);
      const nextRunID = runID && summaryPayload.runs.some((run) => run.id === runID) ? runID : summaryPayload.runs[0]?.id || "";
      setSelectedRunID(nextRunID);
      if (nextRunID) {
        const report = await api.adminOpsEvaluationRun(token, nextRunID, 500);
        const filtered = filterEvaluationResults(report.results, {
          status: resultStatusFilter,
          userID: cleanUserID,
          sessionID: sessionID.trim(),
          jobID: jobID.trim(),
          skillName: skillName.trim(),
          provider: provider.trim(),
          model: model.trim(),
          subjectType
        });
        setResults(filtered);
        setReviews((current) => mergeEvaluationReviews(current, report.reviews));
        setSelectedResultID((current) => {
          if (current && filtered.some((result) => result.id === current)) return current;
          return filtered[0]?.id || "";
        });
      } else {
        setResults([]);
        setSelectedResultID("");
      }
      setNotice(`Loaded ${summaryPayload.runs.length} eval runs`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const createRun = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before running evaluation.");
      return;
    }
    setRunning(true);
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const report = await api.createEvaluationRun(token, {
        name: `${subjectType}_quality_${new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "")}`,
        trigger: "admin_ui",
        scope: {
          from,
          subject_type: subjectType,
          user_id: cleanUserID,
          session_id: sessionID.trim(),
          job_id: jobID.trim(),
          skill_name: skillName.trim(),
          provider: provider.trim(),
          model: model.trim()
        }
      });
      setRuns((current) => [report.run, ...current.filter((run) => run.id !== report.run.id)]);
      setSummary(report.summary);
      setResults(report.results);
      setReviews((current) => mergeEvaluationReviews(current, report.reviews));
      setSelectedRunID(report.run.id);
      setSelectedResultID(report.results[0]?.id || "");
      setNotice(`Evaluation completed: ${report.run.passed} passed, ${report.run.failed} failed, ${report.run.warning} warnings`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRunning(false);
    }
  };

  const openRun = async (runID: string) => {
    setSelectedRunID(runID);
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const report = await api.adminOpsEvaluationRun(token, runID, 500);
      const filtered = filterEvaluationResults(report.results, {
        status: resultStatusFilter,
        userID: cleanUserID,
        sessionID: sessionID.trim(),
        jobID: jobID.trim(),
        skillName: skillName.trim(),
        provider: provider.trim(),
        model: model.trim(),
        subjectType
      });
      setResults(filtered);
      setReviews((current) => mergeEvaluationReviews(current, report.reviews));
      setSelectedResultID(filtered[0]?.id || "");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const updateReview = async (review: EvaluationReview, status: string) => {
    if (!token) return;
    setReviewBusy(review.id);
    setError("");
    try {
      const updated = await api.updateEvaluationReview(token, review.id, {
        status,
        reviewer: "admin",
        note: status === "ignored" ? "ignored from Admin UI" : "reviewed from Admin UI"
      });
      setReviews((current) => mergeEvaluationReviews(current, [updated]));
      setNotice(`Review ${updated.id} marked ${updated.status}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setReviewBusy("");
    }
  };

  const exportResultsCSV = async () => {
    if (!token) return;
    setExportBusy("csv");
    setError("");
    try {
      const content = await api.adminOpsEvaluationResultsCSV(token, {
        runId: selectedRunID || selectedRun?.id,
        status: resultStatusFilter,
        userId: cleanUserID,
        sessionId: sessionID.trim(),
        jobId: jobID.trim(),
        skillName: skillName.trim(),
        provider: provider.trim(),
        model: model.trim(),
        subjectType,
        limit: 1000
      });
      downloadTextFile(`evaluation-results-${selectedRunID || "filtered"}.csv`, content, "text/csv;charset=utf-8");
      setNotice("Evaluation results CSV exported");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setExportBusy("");
    }
  };

  const exportSummaryMarkdown = async () => {
    if (!token) return;
    setExportBusy("markdown");
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const content = await api.adminOpsEvaluationSummaryMarkdown(token, { from, status: runStatusFilter, limit: 500 });
      downloadTextFile("evaluation-summary.md", content, "text/markdown;charset=utf-8");
      setNotice("Evaluation summary Markdown exported");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setExportBusy("");
    }
  };

  const loadGoldenSets = async () => {
    if (!token) return;
    setError("");
    try {
      const sets = await api.adminOpsGoldenSets(token, { limit: 200 });
      setGoldenSets(sets);
      if (!goldenSetID.trim() && sets[0]) {
        setGoldenSetID(sets[0].id);
        setGoldenTargetVersion(sets[0].version || "v1");
      }
      setNotice(`Loaded ${sets.length} golden set versions`);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const createGoldenCases = async () => {
    const cleanSetID = goldenSetID.trim();
    if (!token || !cleanSetID) {
      setError("Enter a Golden Set ID before generating cases.");
      return;
    }
    setGoldenBusy("capture");
    setError("");
    try {
      const payload = await api.createGoldenCasesFromTrace(token, cleanSetID, {
        source_version: goldenSourceVersion.trim(),
        target_version: goldenTargetVersion.trim() || "v1",
        scope: {
          subject_type: goldenTraceSubjectType,
          user_id: cleanUserID,
          session_id: sessionID.trim(),
          job_id: jobID.trim(),
          skill_name: skillName.trim(),
          provider: provider.trim(),
          model: model.trim()
        },
        subject_id: goldenSubjectID.trim(),
        expected_answer: goldenExpectedAnswer.trim(),
        expected_facts: splitGoldenLines(goldenExpectedFacts),
        tags: splitGoldenLines(goldenTags),
        max_cases: Math.max(1, Math.min(100, goldenMaxCases || 1))
      });
      setGoldenSets((current) => [payload.set, ...current.filter((set) => !(set.id === payload.set.id && set.version === payload.set.version))]);
      setGoldenSetID(payload.set.id);
      setGoldenTargetVersion(payload.set.version || goldenTargetVersion || "v1");
      setGoldenSourceVersion(payload.set.version || goldenTargetVersion || "v1");
      setNotice(`Generated ${payload.cases.length} golden cases`);
      setEvaluationTab("golden");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setGoldenBusy("");
    }
  };

  const runGoldenEvaluation = async () => {
    const set = selectedGoldenSet;
    if (!token || !set) {
      setError("Select or generate a Golden Set before running evaluation.");
      return;
    }
    if (!set.cases.length) {
      setError("Selected Golden Set has no cases.");
      return;
    }
    setGoldenBusy("run");
    setError("");
    try {
      const candidates: GoldenCandidate[] = set.cases.map((item) => ({
        case_id: item.id,
        output: item.expected_answer || item.expected_facts?.join("\n") || item.query,
        retrieved_evidence: item.gold_evidence || [],
        metadata: { source: "admin_baseline" }
      }));
      const report = await api.createGoldenEvaluationRun(token, {
        setId: set.id,
        setVersion: set.version || "",
        judge: goldenJudge,
        name: `${set.id}_${set.version || "latest"}_${goldenJudge}_${new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "")}`,
        trigger: "admin_ui",
        candidates
      });
      setRuns((current) => [report.run, ...current.filter((run) => run.id !== report.run.id)]);
      setSummary(report.summary);
      setResults(report.results);
      setReviews((current) => mergeEvaluationReviews(current, report.reviews));
      setSelectedRunID(report.run.id);
      setSelectedResultID(report.results[0]?.id || "");
      setSubjectType("golden_case");
      setEvaluationTab("results");
      setNotice(`Golden evaluation completed: ${report.run.passed} passed, ${report.run.failed} failed, ${report.run.warning} warnings`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setGoldenBusy("");
    }
  };

  useEffect(() => {
    if (token) {
      void loadEvaluation();
      void loadGoldenSets();
    }
  }, [token]);

  const selectedResultReviews = selectedResult ? reviewsByResultID.get(selectedResult.id) || [] : [];
  const metrics = selectedRun?.metrics || summary?.metrics || {};
  const passRate = selectedRun ? selectedRunPassRate(selectedRun) : summary?.pass_rate ?? 0;
  const totalResults = selectedRun?.total ?? summary?.total ?? 0;
  const failedResults = selectedRun?.failed ?? summary?.failed ?? 0;
  const warningResults = selectedRun?.warning ?? summary?.warning ?? 0;
  const p95LatencyMS = metricNumber(metrics, "p95_latency_ms");
  const averageLatencyMS = metricNumber(metrics, "average_latency_ms");
  const chatLLMP95MS = metricNumber(metrics, "chat_llm_full_p95_ms");
  const firstTokenP95MS = metricNumber(metrics, "first_token_p95_ms");
  const jobEndToEndP95MS = metricNumber(metrics, "job_end_to_end_p95_ms");
  const skillExecutionP95MS = metricNumber(metrics, "skill_execution_p95_ms");
  const sandboxStartupP95MS = metricNumber(metrics, "sandbox_startup_p95_ms");
  const artifactGenerationP95MS = metricNumber(metrics, "artifact_generation_p95_ms");
  const totalTokens = metricNumber(metrics, "total_tokens");
  const estimatedCostUSD = metricNumber(metrics, "estimated_cost_usd");
  const toolErrorRate = metricNumber(metrics, "tool_error_rate");
  const llmErrorRate = metricNumber(metrics, "llm_error_rate");
  const answerCorrectness = metricNumber(metrics, "answer_correctness_avg");
  const answerRelevancy = metricNumber(metrics, "answer_relevancy_avg");
  const faithfulness = metricNumber(metrics, "faithfulness_avg");
  const contextPrecision = metricNumber(metrics, "context_precision_avg");
  const contextRecall = metricNumber(metrics, "context_recall_avg");
  const hasRagasMetrics = ["answer_correctness_avg", "answer_relevancy_avg", "faithfulness_avg", "context_precision_avg", "context_recall_avg"].some((key) => metrics[key] != null);
  const evaluationTabs: Array<AdminTabOption<typeof evaluationTab>> = [
    { id: "results", label: "Results", icon: <Activity size={15} />, count: results.length },
    { id: "selected", label: "Selected", icon: <Info size={15} /> },
    { id: "reviews", label: "Reviews", icon: <ShieldCheck size={15} />, count: selectedResultReviews.length },
    { id: "io", label: "I/O", icon: <FileText size={15} /> },
    { id: "golden", label: "Golden", icon: <Database size={15} />, count: selectedGoldenSet?.cases.length || 0 }
  ];

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel evaluation-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID</span>
            <Input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="required for new eval" aria-label="Evaluation user ID" />
          </label>
          <div className="admin-filter-row">
            <select value={subjectType} onChange={(event) => setSubjectType(event.currentTarget.value)} aria-label="Evaluation subject">
              <option value="job">Jobs</option>
              <option value="session">Sessions</option>
              <option value="skill_execution">Skill executions</option>
              <option value="golden_case">Golden cases</option>
            </select>
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Evaluation time window">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
          </div>
          <div className="admin-filter-row">
            <select value={runStatusFilter} onChange={(event) => setRunStatusFilter(event.currentTarget.value)} aria-label="Evaluation run status">
              <option value="all">All runs</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="running">Running</option>
            </select>
            <select value={resultStatusFilter} onChange={(event) => setResultStatusFilter(event.currentTarget.value)} aria-label="Evaluation result status">
              <option value="all">All results</option>
              <option value="failed">Failed</option>
              <option value="warning">Warning</option>
              <option value="passed">Passed</option>
            </select>
          </div>
          <div className="admin-filter-row">
            <label className="admin-field">
              <span>Session ID</span>
              <Input value={sessionID} onChange={(event) => setSessionID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation session ID" />
            </label>
            <label className="admin-field">
              <span>Job ID</span>
              <Input value={jobID} onChange={(event) => setJobID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation job ID" />
            </label>
          </div>
          <label className="admin-field">
            <span>Skill / model</span>
            <Input value={skillName} onChange={(event) => setSkillName(event.currentTarget.value)} placeholder="skill name" aria-label="Evaluation skill name" />
          </label>
          <div className="admin-filter-row">
            <Input value={provider} onChange={(event) => setProvider(event.currentTarget.value)} placeholder="provider" aria-label="Evaluation provider" />
            <Input value={model} onChange={(event) => setModel(event.currentTarget.value)} placeholder="model" aria-label="Evaluation model" />
          </div>
          <div className="admin-action-row compact evaluation-actions">
            <Button className="primary skill-action" onClick={createRun} disabled={running || !token || !cleanUserID}>
              <PlayCircle size={16} />
              <span>{running ? "Running" : "Run eval"}</span>
            </Button>
            <Button className="skill-action" onClick={() => loadEvaluation()} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>{loading ? "Loading" : "Load"}</span>
            </Button>
            <Button className="skill-action" onClick={exportResultsCSV} disabled={exportBusy === "csv" || !token}>
              <Download size={16} />
              <span>{exportBusy === "csv" ? "Exporting" : "CSV"}</span>
            </Button>
            <Button className="skill-action" onClick={exportSummaryMarkdown} disabled={exportBusy === "markdown" || !token}>
              <FileText size={16} />
              <span>{exportBusy === "markdown" ? "Exporting" : "Report"}</span>
            </Button>
          </div>
        </div>
        <div className="admin-skill-list">
          {runs.map((run) => (
            <Button key={run.id} className={`admin-skill-row ${run.id === selectedRun?.id ? "active" : ""}`} onClick={() => openRun(run.id)}>
              <Activity size={18} />
              <span>
                <strong>{run.name}</strong>
                <small>{run.id} · {formatTime(run.completed_at || run.started_at)}</small>
              </span>
              <StatusBadge value={run.status} />
            </Button>
          ))}
          {!runs.length && <div className="empty-small">{loading ? "Loading..." : "No eval runs"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <Button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </Button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>{selectedRun?.name || "Evaluation overview"}</h2>
            <small>{selectedRun ? `${selectedRun.id} · ${selectedRun.scope?.user_id || "user scope"}` : "No run selected"}</small>
          </div>
          {selectedRun && <StatusBadge value={selectedRun.status} />}
        </div>
        <div className="admin-metrics evaluation-metrics">
          <AdminMetric label="Task success" value={formatPercent(passRate)} />
          <AdminMetric label="Results" value={String(totalResults)} />
          <AdminMetric label="Failed / warning" value={`${failedResults} / ${warningResults}`} />
          <AdminMetric label="P95 latency" value={`${formatNumber(Math.round(p95LatencyMS))} ms`} />
          <AdminMetric label="Avg latency" value={`${formatNumber(Math.round(averageLatencyMS))} ms`} />
          <AdminMetric label="TTFT P95" value={`${formatLatencyMetric(firstTokenP95MS)}`} />
          <AdminMetric label="Chat LLM P95" value={`${formatLatencyMetric(chatLLMP95MS)}`} />
          <AdminMetric label="Job P95" value={`${formatLatencyMetric(jobEndToEndP95MS)}`} />
          <AdminMetric label="Skill P95" value={`${formatLatencyMetric(skillExecutionP95MS)}`} />
          <AdminMetric label="Sandbox start P95" value={`${formatLatencyMetric(sandboxStartupP95MS)}`} />
          <AdminMetric label="Artifact P95" value={`${formatLatencyMetric(artifactGenerationP95MS)}`} />
          <AdminMetric label="Token cost" value={formatUSD(estimatedCostUSD)} />
          <AdminMetric label="Tokens" value={formatNumber(totalTokens)} />
          <AdminMetric label="Tool fail rate" value={formatPercent(toolErrorRate)} />
          <AdminMetric label="LLM fail rate" value={formatPercent(llmErrorRate)} />
          {hasRagasMetrics && <AdminMetric label="Answer correctness" value={formatPercent(answerCorrectness)} />}
          {hasRagasMetrics && <AdminMetric label="Answer relevancy" value={formatPercent(answerRelevancy)} />}
          {hasRagasMetrics && <AdminMetric label="Faithfulness" value={formatPercent(faithfulness)} />}
          {hasRagasMetrics && <AdminMetric label="Context precision" value={formatPercent(contextPrecision)} />}
          {hasRagasMetrics && <AdminMetric label="Context recall" value={formatPercent(contextRecall)} />}
          <AdminMetric label="Pending reviews" value={String(reviews.filter((review) => review.status === "pending").length)} />
        </div>
        <AdminTabs tabs={evaluationTabs} active={evaluationTab} onChange={setEvaluationTab} label="Evaluation detail sections" compact />
        <div className="admin-detail-grid">
          {evaluationTab === "results" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Results</h3>
              <small>{results.length} shown</small>
            </div>
            <div className="admin-table">
              {results.slice(0, 24).map((result) => (
                <Button key={result.id} className={`admin-table-row button-row ${result.id === selectedResult?.id ? "active" : ""}`} onClick={() => setSelectedResultID(result.id)}>
                  <StatusBadge value={result.status} />
                  <span>
                    <strong>{result.subject_type}:{result.subject_id}</strong>
                    <small>{[result.user_id, result.session_id, result.job_id, result.skill_name].filter(Boolean).join(" · ") || "runtime record"}</small>
                  </span>
                  <small>{formatNumber(Math.round((result.score || 0) * 100))}</small>
                  {(result.findings || []).slice(0, 2).map((finding) => <em key={`${result.id}-${finding.code}`}>{finding.code}: {finding.message}</em>)}
                </Button>
              ))}
              {!results.length && <p className="muted-text">No results in this filter.</p>}
            </div>
          </section>}
          {evaluationTab === "selected" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Selected result</h3>
              {selectedResult && <StatusBadge value={selectedResult.status} />}
            </div>
            {selectedResult ? (
              <div className="admin-facts">
                <SkillFact label="Subject" value={`${selectedResult.subject_type}:${selectedResult.subject_id}`} />
                <SkillFact label="Score" value={String(selectedResult.score)} />
                <SkillFact label="Provider" value={[selectedResult.provider, selectedResult.model].filter(Boolean).join(" / ") || "none"} />
                <SkillFact label="Created" value={formatTime(selectedResult.created_at)} />
                <SkillFact label="Session" value={selectedResult.session_id || "none"} />
                <SkillFact label="Job" value={selectedResult.job_id || "none"} />
                {selectedResult.subject_type === "golden_case" && <SkillFact label="RAGAS" value={[
                  `correct ${formatPercent(metricNumber(selectedResult.metrics || {}, "answer_correctness"))}`,
                  `faith ${formatPercent(metricNumber(selectedResult.metrics || {}, "faithfulness"))}`,
                  `recall ${formatPercent(metricNumber(selectedResult.metrics || {}, "context_recall"))}`
                ].join(" · ")} />}
              </div>
            ) : (
              <p className="muted-text">Select a result to inspect findings.</p>
            )}
          </section>}
          {evaluationTab === "selected" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Findings</h3>
            </div>
            <div className="admin-table">
              {(selectedResult?.findings || []).map((finding) => (
                <div key={`${finding.code}-${finding.message}`} className={`review-issue ${finding.severity}`}>
                  <strong>{finding.code}</strong>
                  <span>{finding.message}</span>
                </div>
              ))}
              {selectedResult && !selectedResult.findings?.length && <p className="muted-text">No findings for this result.</p>}
            </div>
          </section>}
          {evaluationTab === "reviews" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Review items</h3>
            </div>
            <div className="admin-table">
              {selectedResultReviews.map((review) => (
                <div key={review.id} className="admin-table-row">
                  <StatusBadge value={review.status} />
                  <span>
                    <strong>{review.id}</strong>
                    <small>{review.note || "No note"}</small>
                  </span>
                  <small>{formatTime(review.updated_at)}</small>
                  <Button className="small ghost" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "passed")}>Pass</Button>
                  <Button className="small danger" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "ignored")}>Ignore</Button>
                </div>
              ))}
              {!selectedResultReviews.length && <p className="muted-text">No review items for the selected result.</p>}
            </div>
          </section>}
          {evaluationTab === "io" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Input / output</h3>
            </div>
            <pre className="admin-code-block">{selectedResult ? JSON.stringify({
              input: selectedResult.input || "",
              output: selectedResult.output || "",
              metrics: selectedResult.metrics || {}
            }, null, 2) : "{}"}</pre>
          </section>}
          {evaluationTab === "golden" && <section className="admin-card wide golden-admin-card">
            <div className="admin-card-head">
              <h3>Golden sets</h3>
              <Button className="small ghost" onClick={loadGoldenSets} disabled={goldenBusy === "load" || !token}>
                <RefreshCw size={14} />
                <span>Refresh</span>
              </Button>
            </div>
            <div className="golden-form-grid">
              <label className="admin-field">
                <span>Existing set</span>
                <select
                  value={selectedGoldenSetKey}
                  onChange={(event) => {
                    const [id, version] = event.currentTarget.value.split("::");
                    setGoldenSetID(id || "");
                    setGoldenTargetVersion(version || "v1");
                    setGoldenSourceVersion(version || "");
                  }}
                  aria-label="Existing Golden Set"
                >
                  <option value="">No set selected</option>
                  {goldenSets.map((set) => (
                    <option key={`${set.id}::${set.version || ""}`} value={`${set.id}::${set.version || ""}`}>
                      {set.id} · {set.version || "latest"} · {set.cases.length} cases
                    </option>
                  ))}
                </select>
              </label>
              <label className="admin-field">
                <span>Set ID</span>
                <Input value={goldenSetID} onChange={(event) => setGoldenSetID(event.currentTarget.value)} placeholder="runtime-golden" aria-label="Golden Set ID" />
              </label>
              <label className="admin-field">
                <span>Source version</span>
                <Input value={goldenSourceVersion} onChange={(event) => setGoldenSourceVersion(event.currentTarget.value)} placeholder="optional" aria-label="Golden source version" />
              </label>
              <label className="admin-field">
                <span>Target version</span>
                <Input value={goldenTargetVersion} onChange={(event) => setGoldenTargetVersion(event.currentTarget.value)} placeholder="v1" aria-label="Golden target version" />
              </label>
              <label className="admin-field">
                <span>Trace subject</span>
                <select value={goldenTraceSubjectType} onChange={(event) => setGoldenTraceSubjectType(event.currentTarget.value)} aria-label="Golden trace subject">
                  <option value="job">Jobs</option>
                  <option value="session">Sessions</option>
                  <option value="skill_execution">Skill executions</option>
                </select>
              </label>
              <label className="admin-field">
                <span>Subject ID</span>
                <Input value={goldenSubjectID} onChange={(event) => setGoldenSubjectID(event.currentTarget.value)} placeholder="job/session id" aria-label="Golden trace subject ID" />
              </label>
              <label className="admin-field">
                <span>Max cases</span>
                <Input type="number" min={1} max={100} value={String(goldenMaxCases)} onChange={(event) => setGoldenMaxCases(Number(event.currentTarget.value))} aria-label="Golden max cases" />
              </label>
              <label className="admin-field">
                <span>Tags</span>
                <Input value={goldenTags} onChange={(event) => setGoldenTags(event.currentTarget.value)} placeholder="from-runtime, smoke" aria-label="Golden tags" />
              </label>
              <label className="admin-field wide">
                <span>Expected answer override</span>
                <Textarea value={goldenExpectedAnswer} onChange={(event) => setGoldenExpectedAnswer(event.currentTarget.value)} rows={3} placeholder="optional" aria-label="Golden expected answer override" />
              </label>
              <label className="admin-field wide">
                <span>Expected facts</span>
                <Textarea value={goldenExpectedFacts} onChange={(event) => setGoldenExpectedFacts(event.currentTarget.value)} rows={3} placeholder="one fact per line" aria-label="Golden expected facts" />
              </label>
            </div>
            <div className="admin-action-row golden-actions">
              <Button className="primary skill-action" onClick={createGoldenCases} disabled={goldenBusy === "capture" || !token || !goldenSetID.trim()}>
                <Archive size={16} />
                <span>{goldenBusy === "capture" ? "Generating" : "Generate cases"}</span>
              </Button>
              <select value={goldenJudge} onChange={(event) => setGoldenJudge(event.currentTarget.value)} aria-label="Golden judge">
                <option value="heuristic">Heuristic judge</option>
                <option value="llm">LLM judge</option>
              </select>
              <Button className="skill-action" onClick={runGoldenEvaluation} disabled={goldenBusy === "run" || !token || !selectedGoldenSet?.cases.length}>
                <Sparkles size={16} />
                <span>{goldenBusy === "run" ? "Running" : "Run golden eval"}</span>
              </Button>
            </div>
            <div className="admin-table golden-case-list">
              {selectedGoldenSet ? (
                <>
                  <div className="admin-table-row">
                    <StatusBadge value={selectedGoldenSet.cases.length ? "ready" : "empty"} />
                    <span>
                      <strong>{selectedGoldenSet.id} · {selectedGoldenSet.version || "latest"}</strong>
                      <small>{selectedGoldenSet.name || "Golden set"} · {selectedGoldenSet.updated_at ? formatTime(selectedGoldenSet.updated_at) : "not persisted"}</small>
                    </span>
                    <small>{selectedGoldenSet.cases.length} cases</small>
                  </div>
                  {selectedGoldenSet.cases.slice(0, 8).map((item) => (
                    <div key={item.id} className="admin-table-row">
                      <FileText size={16} />
                      <span>
                        <strong>{item.query}</strong>
                        <small>{item.expected_answer || item.expected_facts?.join(" · ") || item.id}</small>
                      </span>
                      <small>{item.tags?.join(", ") || "case"}</small>
                    </div>
                  ))}
                </>
              ) : (
                <p className="muted-text">No Golden Set loaded.</p>
              )}
            </div>
          </section>}
        </div>
      </section>
    </div>
  );
}
