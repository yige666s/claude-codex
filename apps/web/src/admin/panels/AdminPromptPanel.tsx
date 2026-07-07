import { useEffect, useMemo, useState } from "react";
import { Archive, Beaker, CheckCircle2, Code2, GitCompare, PlayCircle, RefreshCw, Rocket, Search, ShieldCheck, Split, X } from "lucide-react";
import { ApiClient } from "../../api/client";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Textarea } from "../../components/ui/textarea";
import {
  AdminDetailPanel,
  AdminEmptyState,
  AdminListPanel,
  AdminSearchBox,
  AdminSectionNotice,
  AdminSplitPane
} from "../ui";
import {
  AdminMetric,
  AdminTabs,
  SkillFact,
  StatusBadge,
  errorMessage,
  formatNumber,
  formatPercent,
  formatTime,
  formatUSD,
  fuzzyMatch,
  metricNumber,
  type AdminTabOption
} from "../shared";
import type {
  EvaluationResult,
  EvaluationRunReport,
  GoldenCandidate,
  GoldenSet,
  LLMUsageAdminSummary,
  PromptDetail,
  PromptEnvironmentPin,
  PromptExperiment,
  PromptRenderResult,
  PromptTemplate,
  PromptVersion,
  PromptVersionDiff
} from "../../types";

const promptEnvironments = ["dev", "staging", "production"];

type PromptTab = "workflow" | "versions" | "preview" | "eval" | "experiments" | "usage";

function promptDefaultGoldenSetID(promptID: string): string {
  if (promptID === "runtime/deep_agent/planner") return "deep_agent_prompt_planner";
  if (promptID === "runtime/deep_agent/router") return "deep_agent_prompt_router";
  return "runtime-golden";
}

function promptShortHash(value = ""): string {
  return value ? value.slice(0, 12) : "none";
}

function safeJSON(value: string): Record<string, unknown> {
  const text = value.trim();
  if (!text) return {};
  const parsed = JSON.parse(text) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Variables must be a JSON object");
  return parsed as Record<string, unknown>;
}

function splitList(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function formatDiffValue(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "string") return value;
  return JSON.stringify(value, null, 2);
}

function versionLabel(version: PromptVersion | undefined): string {
  if (!version) return "";
  return `${version.version} · ${version.status}`;
}

function latestVersionFor(detail: PromptDetail | null): PromptVersion | undefined {
  return detail?.versions?.[0] || detail?.published_version;
}

function publishedVersionFor(detail: PromptDetail | null): PromptVersion | undefined {
  return detail?.published_version || detail?.versions.find((item) => item.status === "published") || detail?.versions[0];
}

function envPinFor(pins: PromptEnvironmentPin[], environment: string): PromptEnvironmentPin | undefined {
  return pins.find((pin) => pin.environment === environment);
}

export function AdminPromptPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const token = adminToken.trim();
  const [prompts, setPrompts] = useState<PromptTemplate[]>([]);
  const [selectedPromptID, setSelectedPromptID] = useState("");
  const [detail, setDetail] = useState<PromptDetail | null>(null);
  const [query, setQuery] = useState("");
  const [scopeFilter, setScopeFilter] = useState("all");
  const [statusFilter, setStatusFilter] = useState("all");
  const [tab, setTab] = useState<PromptTab>("workflow");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [baseVersion, setBaseVersion] = useState("");
  const [targetVersion, setTargetVersion] = useState("");
  const [candidateVersion, setCandidateVersion] = useState("");
  const [candidateContent, setCandidateContent] = useState("");
  const [candidateChangelog, setCandidateChangelog] = useState("");
  const [previewVariables, setPreviewVariables] = useState("{}");
  const [preview, setPreview] = useState<PromptRenderResult | null>(null);
  const [diff, setDiff] = useState<PromptVersionDiff | null>(null);
  const [goldenSets, setGoldenSets] = useState<GoldenSet[]>([]);
  const [goldenSetID, setGoldenSetID] = useState("");
  const [goldenSetVersion, setGoldenSetVersion] = useState("");
  const [goldenJudge, setGoldenJudge] = useState("heuristic");
  const [evalRun, setEvalRun] = useState<EvaluationRunReport | null>(null);
  const [evalRunID, setEvalRunID] = useState("");
  const [pinChangelog, setPinChangelog] = useState("");
  const [experimentID, setExperimentID] = useState("");
  const [experimentName, setExperimentName] = useState("");
  const [experimentControlVersion, setExperimentControlVersion] = useState("");
  const [experimentCandidateVersion, setExperimentCandidateVersion] = useState("");
  const [experimentWeight, setExperimentWeight] = useState(50);
  const [experiments, setExperiments] = useState<PromptExperiment[]>([]);
  const [selectedExperimentID, setSelectedExperimentID] = useState("");
  const [usage, setUsage] = useState<LLMUsageAdminSummary | null>(null);
  const [evalResults, setEvalResults] = useState<EvaluationResult[]>([]);

  const selectedPrompt = prompts.find((prompt) => prompt.id === selectedPromptID) || detail?.prompt || null;
  const versions = detail?.versions || [];
  const selectedVersion = versions.find((version) => version.version === targetVersion) || latestVersionFor(detail);
  const selectedGoldenSet = goldenSets.find((set) => set.id === goldenSetID && (!goldenSetVersion || set.version === goldenSetVersion))
    || goldenSets.find((set) => set.id === goldenSetID)
    || null;
  const envPins = detail?.env_pins || [];
  const publishedVersion = publishedVersionFor(detail);
  const workflowDone = {
    candidate: Boolean(versions.find((version) => version.version === targetVersion && version.status !== "published")),
    preview: Boolean(preview && preview.prompt_version === targetVersion),
    eval: Boolean(evalRunID),
    staging: envPinFor(envPins, "staging")?.version === targetVersion,
    production: envPinFor(envPins, "production")?.version === targetVersion
  };
  const tabs: Array<AdminTabOption<PromptTab>> = [
    { id: "workflow", label: "Workflow", icon: <Rocket size={15} /> },
    { id: "versions", label: "Versions", icon: <Archive size={15} />, count: versions.length },
    { id: "preview", label: "Preview", icon: <Code2 size={15} /> },
    { id: "eval", label: "Eval", icon: <ShieldCheck size={15} /> },
    { id: "experiments", label: "Experiment", icon: <Beaker size={15} />, count: experiments.length },
    { id: "usage", label: "Usage", icon: <Split size={15} /> }
  ];
  const filteredPrompts = useMemo(() => prompts.filter((prompt) => {
    const scopeMatches = scopeFilter === "all" || (prompt.scope || "") === scopeFilter;
    return scopeMatches && fuzzyMatch(query, [prompt.id, prompt.name, prompt.description, prompt.scope, prompt.owner]);
  }), [prompts, query, scopeFilter]);
  const scopes = useMemo(() => Array.from(new Set(prompts.map((prompt) => prompt.scope).filter(Boolean) as string[])).sort(), [prompts]);

  const setPromptDefaults = (payload: PromptDetail) => {
    const published = publishedVersionFor(payload);
    const latest = latestVersionFor(payload);
    const candidate = payload.versions.find((item) => item.status !== "published" && item.version !== published?.version)
      || payload.versions.find((item) => item.version !== published?.version)
      || latest
      || published;
    setBaseVersion((current) => current || published?.version || latest?.version || "");
    setTargetVersion((current) => current || candidate?.version || latest?.version || "");
    setCandidateVersion((current) => current || `candidate-${new Date().toISOString().slice(0, 10).replace(/-/g, "")}`);
    setCandidateContent((current) => current || candidate?.content || published?.content || latest?.content || "");
    setExperimentControlVersion((current) => current || published?.version || latest?.version || "");
    setExperimentCandidateVersion((current) => current || candidate?.version || latest?.version || "");
    setGoldenSetID((current) => current || promptDefaultGoldenSetID(payload.prompt.id));
  };

  const loadPromptCatalog = async (selectID = selectedPromptID) => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const next = await api.adminOpsPrompts(token, {
        status: statusFilter,
        q: query.trim(),
        scope: scopeFilter,
        limit: 300
      });
      setPrompts(next);
      const nextID = selectID && next.some((prompt) => prompt.id === selectID) ? selectID : next[0]?.id || "";
      setSelectedPromptID(nextID);
      if (nextID) await loadPromptDetail(nextID, true);
      setNotice(`Loaded ${next.length} prompts`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const loadPromptDetail = async (promptID = selectedPromptID, quiet = false) => {
    const cleanID = promptID.trim();
    if (!token || !cleanID) return;
    if (!quiet) setBusy("detail");
    setError("");
    try {
      const payload = await api.adminOpsPrompt(token, cleanID);
      setDetail(payload);
      setSelectedPromptID(payload.prompt.id);
      setPromptDefaults(payload);
      await Promise.all([
        loadPromptExperiments(payload.prompt.id, true),
        loadUsage(payload.prompt.id, targetVersion || latestVersionFor(payload)?.version || "", true)
      ]);
      if (!quiet) setNotice(`Loaded ${payload.prompt.id}`);
    } catch (err) {
      setError(errorMessage(err));
      setDetail(null);
    } finally {
      if (!quiet) setBusy("");
    }
  };

  const loadGoldenSets = async () => {
    if (!token) return;
    setBusy("golden");
    setError("");
    try {
      const sets = await api.adminOpsGoldenSets(token, { limit: 200 });
      setGoldenSets(sets);
      const preferredID = selectedPromptID ? promptDefaultGoldenSetID(selectedPromptID) : goldenSetID;
      const preferred = sets.find((set) => set.id === preferredID) || sets[0];
      if (preferred) {
        setGoldenSetID((current) => current || preferred.id);
        setGoldenSetVersion((current) => current || preferred.version || "");
      }
      setNotice(`Loaded ${sets.length} golden set versions`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const loadPromptExperiments = async (promptID = selectedPromptID, quiet = false) => {
    const cleanID = promptID.trim();
    if (!token || !cleanID) return;
    if (!quiet) setBusy("experiments");
    setError("");
    try {
      const next = await api.adminOpsPromptExperiments(token, { promptId: cleanID, limit: 100 });
      setExperiments(next);
      setSelectedExperimentID((current) => current && next.some((item) => item.id === current) ? current : next[0]?.id || "");
    } catch (err) {
      if (!quiet) setError(errorMessage(err));
    } finally {
      if (!quiet) setBusy("");
    }
  };

  const loadUsage = async (promptID = selectedPromptID, version = targetVersion, quiet = false) => {
    const cleanID = promptID.trim();
    if (!token || !cleanID) return;
    if (!quiet) setBusy("usage");
    setError("");
    try {
      const [nextUsage, nextResults] = await Promise.all([
        api.adminOpsLLMUsage(token, { promptId: cleanID, promptVersion: version.trim(), days: 14, limit: 40 }),
        api.adminOpsEvaluationResults(token, { promptId: cleanID, promptVersion: version.trim(), limit: 40 })
      ]);
      setUsage(nextUsage);
      setEvalResults(nextResults);
      if (!quiet) setNotice(`Loaded prompt drilldown for ${cleanID}`);
    } catch (err) {
      if (!quiet) setError(errorMessage(err));
    } finally {
      if (!quiet) setBusy("");
    }
  };

  useEffect(() => {
    if (token) void loadPromptCatalog();
  }, []);

  useEffect(() => {
    if (token && selectedPromptID) void loadPromptDetail(selectedPromptID, true);
  }, [selectedPromptID]);

  useEffect(() => {
    if (token) void loadGoldenSets();
  }, []);

  const selectPrompt = (promptID: string) => {
    setSelectedPromptID(promptID);
    setDetail(null);
    setPreview(null);
    setDiff(null);
    setEvalRun(null);
    setEvalRunID("");
    setBaseVersion("");
    setTargetVersion("");
    setCandidateContent("");
    setCandidateVersion("");
    setCandidateChangelog("");
    setExperimentID("");
    setExperimentName("");
    setExperimentControlVersion("");
    setExperimentCandidateVersion("");
  };

  const createCandidate = async () => {
    if (!token || !selectedPromptID.trim() || !candidateVersion.trim() || !candidateContent.trim()) {
      setError("Prompt, version, and content are required.");
      return;
    }
    setBusy("create");
    setError("");
    try {
      const created = await api.createPromptVersion(token, selectedPromptID, {
        version: candidateVersion.trim(),
        status: "review_pending",
        content: candidateContent,
        base_version: baseVersion.trim(),
        changelog: candidateChangelog.trim()
      });
      setTargetVersion(created.version);
      setNotice(`Candidate ${created.version} created`);
      await loadPromptDetail(selectedPromptID, true);
      setTab("preview");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const renderPreview = async () => {
    if (!token || !selectedPromptID || !targetVersion) return;
    setBusy("preview");
    setError("");
    try {
      const rendered = await api.renderPromptVersionPreview(token, selectedPromptID, targetVersion, safeJSON(previewVariables));
      setPreview(rendered);
      setNotice(`Preview rendered for ${targetVersion}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const loadDiff = async () => {
    if (!token || !selectedPromptID || !baseVersion || !targetVersion) return;
    setBusy("diff");
    setError("");
    try {
      const next = await api.diffPromptVersions(token, selectedPromptID, baseVersion, targetVersion);
      setDiff(next);
      setNotice(`Diff loaded: ${next.diff.length} changed fields`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const runPromptEval = async () => {
    if (!token || !selectedPromptID || !targetVersion || !selectedGoldenSet) {
      setError("Select a prompt version and Golden Set before running eval.");
      return;
    }
    if (!selectedGoldenSet.cases.length) {
      setError("Selected Golden Set has no cases.");
      return;
    }
    setBusy("eval");
    setError("");
    try {
      const candidates: GoldenCandidate[] = selectedGoldenSet.cases.map((item) => ({
        case_id: item.id,
        output: item.expected_answer || item.expected_facts?.join("\n") || item.query,
        retrieved_evidence: item.gold_evidence || [],
        metadata: { source: "admin_prompt_panel" }
      }));
      const now = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "");
      const report = await api.createPromptVersionEvaluationRun(token, selectedPromptID, targetVersion, {
        setId: selectedGoldenSet.id,
        setVersion: selectedGoldenSet.version || "",
        judge: goldenJudge,
        name: `${selectedPromptID}_${targetVersion}_${now}`,
        trigger: "admin_prompt_panel",
        candidates
      });
      setEvalRun(report);
      setEvalRunID(report.run.id);
      setNotice(`Eval complete: ${report.run.passed} passed, ${report.run.failed} failed, ${report.run.warning} warnings`);
      await loadUsage(selectedPromptID, targetVersion, true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const publishVersion = async () => {
    if (!token || !selectedPromptID || !targetVersion) return;
    setBusy("publish");
    setError("");
    try {
      await api.publishPromptVersion(token, selectedPromptID, targetVersion, candidateChangelog.trim() || `publish ${targetVersion}`);
      setNotice(`Published ${targetVersion}`);
      await loadPromptDetail(selectedPromptID, true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const moveEnvPin = async (environment: string, rollback = false) => {
    if (!token || !selectedPromptID || !targetVersion) return;
    setBusy(`${rollback ? "rollback" : "pin"}-${environment}`);
    setError("");
    try {
      const payload = {
        version: targetVersion,
        changelog: pinChangelog.trim() || `${rollback ? "rollback" : "promote"} ${environment} to ${targetVersion}`,
        evalRunId: evalRunID.trim()
      };
      if (rollback) {
        await api.rollbackPromptEnvironmentPin(token, selectedPromptID, environment, payload);
      } else {
        await api.setPromptEnvironmentPin(token, selectedPromptID, environment, payload);
      }
      setNotice(`${environment} now points to ${targetVersion}`);
      await loadPromptDetail(selectedPromptID, true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const rollbackPublished = async () => {
    if (!token || !selectedPromptID || !targetVersion) return;
    setBusy("rollback-published");
    setError("");
    try {
      await api.rollbackPromptVersion(token, selectedPromptID, targetVersion, candidateChangelog.trim() || `rollback to ${targetVersion}`);
      setNotice(`Published pointer rolled back to ${targetVersion}`);
      await loadPromptDetail(selectedPromptID, true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const createExperiment = async () => {
    if (!token || !selectedPromptID || !experimentControlVersion || !experimentCandidateVersion) {
      setError("Select control and candidate versions before creating an experiment.");
      return;
    }
    const cleanID = experimentID.trim() || `${selectedPromptID.replace(/[^a-zA-Z0-9]+/g, "-")}-${experimentCandidateVersion}-exp`;
    const candidateWeight = Math.max(1, Math.min(99, experimentWeight || 50));
    setBusy("experiment-create");
    setError("");
    try {
      const detail = await api.upsertPromptExperiment(token, {
        experiment: {
          id: cleanID,
          name: experimentName.trim() || cleanID,
          prompt_id: selectedPromptID,
          status: "draft",
          traffic_scope: "user"
        },
        variants: [
          { experiment_id: cleanID, variant_id: "control", prompt_version: experimentControlVersion, weight: 100 - candidateWeight },
          { experiment_id: cleanID, variant_id: "candidate", prompt_version: experimentCandidateVersion, weight: candidateWeight }
        ]
      });
      setExperiments((current) => [detail.experiment, ...current.filter((item) => item.id !== detail.experiment.id)]);
      setSelectedExperimentID(detail.experiment.id);
      setNotice(`Experiment ${detail.experiment.id} saved`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  const updateExperiment = async (action: "start" | "pause" | "complete") => {
    if (!token || !selectedExperimentID) return;
    setBusy(`experiment-${action}`);
    setError("");
    try {
      const detail = await api.updatePromptExperimentStatus(token, selectedExperimentID, action);
      setExperiments((current) => [detail.experiment, ...current.filter((item) => item.id !== detail.experiment.id)]);
      setNotice(`Experiment ${detail.experiment.id} ${detail.experiment.status}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy("");
    }
  };

  return (
    <AdminSplitPane>
      <AdminListPanel>
        <div className="admin-list-tools">
          <AdminSearchBox icon={<Search size={16} />}>
            <Input value={query} onChange={(event) => setQuery(event.currentTarget.value)} placeholder="Search prompts" aria-label="Search prompts" />
          </AdminSearchBox>
          <select value={scopeFilter} onChange={(event) => setScopeFilter(event.currentTarget.value)} aria-label="Prompt scope">
            <option value="all">All scopes</option>
            {scopes.map((scope) => <option key={scope} value={scope}>{scope}</option>)}
          </select>
          <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Prompt status">
            <option value="all">All status</option>
            <option value="draft">Draft</option>
            <option value="review_pending">Review</option>
            <option value="published">Published</option>
            <option value="archived">Archived</option>
          </select>
          <Button className="skill-action" onClick={() => loadPromptCatalog()} disabled={loading || !token}>
            <RefreshCw size={16} />
            <span>{loading ? "Loading" : "Load"}</span>
          </Button>
        </div>
        <div className="admin-skill-list">
          {filteredPrompts.map((prompt) => (
            <Button key={prompt.id} className={`admin-skill-row ${prompt.id === selectedPromptID ? "active" : ""}`} onClick={() => selectPrompt(prompt.id)}>
              <Code2 size={18} />
              <span>
                <strong>{prompt.name || prompt.id}</strong>
                <small>{prompt.id} · {prompt.scope || "runtime"}</small>
              </span>
              <StatusBadge value={prompt.scope || "prompt"} />
            </Button>
          ))}
          {!filteredPrompts.length && <div className="empty-small">{loading ? "Loading..." : "No prompts"}</div>}
        </div>
      </AdminListPanel>
      <AdminDetailPanel>
        {(error || notice) && (
          <AdminSectionNotice tone={error ? "destructive" : "success"} onDismiss={() => { setError(""); setNotice(""); }}>
            {error || notice}
          </AdminSectionNotice>
        )}
        {!selectedPrompt || !detail ? (
          <AdminEmptyState icon={<Code2 size={24} />} title={busy === "detail" ? "Loading prompt" : "Select a prompt"}>
            Prompt registry records appear after loading the catalog.
          </AdminEmptyState>
        ) : (
          <>
            <div className="admin-skill-head">
              <div>
                <h2>{selectedPrompt.name || selectedPrompt.id}</h2>
                <small>{selectedPrompt.id} · {selectedPrompt.scope || "runtime"} · {versions.length} versions</small>
              </div>
              <StatusBadge value={selectedVersion?.status || "unselected"} />
            </div>
            <div className="admin-metrics compact">
              <AdminMetric label="Published" value={publishedVersion?.version || "none"} />
              <AdminMetric label="Candidate" value={targetVersion || "none"} />
              <AdminMetric label="Eval" value={evalRunID ? "ready" : "missing"} />
              <AdminMetric label="Production" value={envPinFor(envPins, "production")?.version || "none"} />
            </div>
            <AdminTabs tabs={tabs} active={tab} onChange={setTab} label="Prompt detail sections" compact />
            <div className="admin-detail-grid">
              {tab === "workflow" && (
                <>
                  <section className="admin-card wide">
                    <div className="admin-card-head">
                      <h3>Release path</h3>
                      <Button className="small ghost" onClick={() => loadPromptDetail(selectedPromptID)} disabled={busy === "detail"}>
                        <RefreshCw size={14} />
                        <span>Refresh</span>
                      </Button>
                    </div>
                    <div className="prompt-workflow-steps">
                      {[
                        ["Candidate", workflowDone.candidate],
                        ["Preview", workflowDone.preview],
                        ["Eval", workflowDone.eval],
                        ["Staging", workflowDone.staging],
                        ["Production", workflowDone.production]
                      ].map(([label, done]) => (
                        <div key={String(label)} className={`prompt-workflow-step ${done ? "done" : ""}`}>
                          <CheckCircle2 size={18} />
                          <strong>{label}</strong>
                        </div>
                      ))}
                    </div>
                  </section>
                  <section className="admin-card wide">
                    <div className="admin-card-head">
                      <h3>Candidate version</h3>
                    </div>
                    <div className="golden-form-grid prompt-editor-grid">
                      <label className="admin-field">
                        <span>Base</span>
                        <select value={baseVersion} onChange={(event) => setBaseVersion(event.currentTarget.value)}>
                          <option value="">No base</option>
                          {versions.map((version) => <option key={`base-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                        </select>
                      </label>
                      <label className="admin-field">
                        <span>Candidate</span>
                        <Input value={candidateVersion} onChange={(event) => setCandidateVersion(event.currentTarget.value)} placeholder="candidate-v1" />
                      </label>
                      <label className="admin-field wide">
                        <span>Changelog</span>
                        <Input value={candidateChangelog} onChange={(event) => setCandidateChangelog(event.currentTarget.value)} placeholder="Release note" />
                      </label>
                      <label className="admin-field wide">
                        <span>Content</span>
                        <Textarea value={candidateContent} onChange={(event) => setCandidateContent(event.currentTarget.value)} rows={12} />
                      </label>
                    </div>
                    <div className="admin-action-row">
                      <Button className="primary skill-action" onClick={createCandidate} disabled={busy === "create"}>
                        <Archive size={16} />
                        <span>{busy === "create" ? "Creating" : "Create candidate"}</span>
                      </Button>
                      <Button className="skill-action" onClick={() => {
                        const source = versions.find((version) => version.version === baseVersion) || publishedVersion || versions[0];
                        setCandidateContent(source?.content || "");
                      }}>
                        <Code2 size={16} />
                        <span>Use base content</span>
                      </Button>
                    </div>
                  </section>
                  <section className="admin-card">
                    <div className="admin-card-head"><h3>Environment pins</h3></div>
                    <div className="admin-table">
                      {promptEnvironments.map((environment) => {
                        const pin = envPinFor(envPins, environment);
                        return (
                          <div key={environment} className="admin-table-row">
                            <StatusBadge value={environment} />
                            <span>
                              <strong>{pin?.version || "unassigned"}</strong>
                              <small>{pin?.eval_run_id || pin?.changelog || "no eval run"}</small>
                            </span>
                            <small>{pin?.updated_at ? formatTime(pin.updated_at) : ""}</small>
                          </div>
                        );
                      })}
                    </div>
                  </section>
                  <section className="admin-card">
                    <div className="admin-card-head"><h3>Promotion</h3></div>
                    <div className="golden-form-grid single">
                      <label className="admin-field wide">
                        <span>Target version</span>
                        <select value={targetVersion} onChange={(event) => setTargetVersion(event.currentTarget.value)}>
                          <option value="">Select version</option>
                          {versions.map((version) => <option key={`target-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                        </select>
                      </label>
                      <label className="admin-field wide">
                        <span>Eval run</span>
                        <Input value={evalRunID} onChange={(event) => setEvalRunID(event.currentTarget.value)} placeholder="required for DeepAgent production" />
                      </label>
                      <label className="admin-field wide">
                        <span>Changelog</span>
                        <Input value={pinChangelog} onChange={(event) => setPinChangelog(event.currentTarget.value)} placeholder="Promotion note" />
                      </label>
                    </div>
                    <div className="admin-action-row">
                      <Button className="skill-action" onClick={() => moveEnvPin("staging")} disabled={busy === "pin-staging" || !targetVersion}>
                        <PlayCircle size={16} />
                        <span>Staging</span>
                      </Button>
                      <Button className="primary skill-action" onClick={() => moveEnvPin("production")} disabled={busy === "pin-production" || !targetVersion}>
                        <Rocket size={16} />
                        <span>Production</span>
                      </Button>
                      <Button className="skill-action" onClick={publishVersion} disabled={busy === "publish" || !targetVersion}>Publish</Button>
                      <Button className="danger-outline skill-action" onClick={rollbackPublished} disabled={busy === "rollback-published" || !targetVersion}>Rollback published</Button>
                    </div>
                  </section>
                </>
              )}
              {tab === "versions" && (
                <section className="admin-card wide">
                  <div className="admin-card-head">
                    <h3>Versions</h3>
                    <Button className="small ghost" onClick={loadDiff} disabled={busy === "diff" || !baseVersion || !targetVersion}>
                      <GitCompare size={14} />
                      <span>{busy === "diff" ? "Loading" : "Diff"}</span>
                    </Button>
                  </div>
                  <div className="golden-form-grid">
                    <label className="admin-field">
                      <span>From</span>
                      <select value={baseVersion} onChange={(event) => setBaseVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`diff-from-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>To</span>
                      <select value={targetVersion} onChange={(event) => setTargetVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`diff-to-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                  </div>
                  <div className="admin-table">
                    {versions.map((version) => (
                      <Button key={`${version.version}-${version.content_hash}`} className={`admin-table-row button-row ${version.version === targetVersion ? "active" : ""}`} onClick={() => {
                        setTargetVersion(version.version);
                        setCandidateContent(version.content || "");
                      }}>
                        <StatusBadge value={version.status} />
                        <span>
                          <strong>{version.version}</strong>
                          <small>{promptShortHash(version.content_hash)} · {version.changelog || "no changelog"}</small>
                        </span>
                        <small>{formatTime(version.published_at || version.created_at || "")}</small>
                      </Button>
                    ))}
                  </div>
                  {diff && (
                    <div className="prompt-diff-list">
                      {diff.diff.map((row) => (
                        <div key={row.field} className="prompt-diff-row">
                          <strong>{row.field}</strong>
                          <pre>{formatDiffValue(row.from)}</pre>
                          <pre>{formatDiffValue(row.to)}</pre>
                        </div>
                      ))}
                      {!diff.diff.length && <p className="muted-text">No changed fields.</p>}
                    </div>
                  )}
                </section>
              )}
              {tab === "preview" && (
                <section className="admin-card wide">
                  <div className="admin-card-head">
                    <h3>Render preview</h3>
                    <Button className="primary small" onClick={renderPreview} disabled={busy === "preview" || !targetVersion}>
                      <PlayCircle size={14} />
                      <span>{busy === "preview" ? "Rendering" : "Render"}</span>
                    </Button>
                  </div>
                  <div className="golden-form-grid">
                    <label className="admin-field">
                      <span>Version</span>
                      <select value={targetVersion} onChange={(event) => setTargetVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`preview-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Hash</span>
                      <Input value={selectedVersion?.content_hash || ""} readOnly />
                    </label>
                    <label className="admin-field wide">
                      <span>Variables JSON</span>
                      <Textarea value={previewVariables} onChange={(event) => setPreviewVariables(event.currentTarget.value)} rows={5} />
                    </label>
                  </div>
                  <pre className="admin-code-block prompt-preview-block">{preview?.rendered_preview || preview?.content || selectedVersion?.content || ""}</pre>
                  {preview?.missing_variables?.length ? <p className="muted-text">Missing: {preview.missing_variables.join(", ")}</p> : null}
                </section>
              )}
              {tab === "eval" && (
                <section className="admin-card wide">
                  <div className="admin-card-head">
                    <h3>Golden eval</h3>
                    <Button className="small ghost" onClick={loadGoldenSets} disabled={busy === "golden"}>
                      <RefreshCw size={14} />
                      <span>Golden sets</span>
                    </Button>
                  </div>
                  <div className="golden-form-grid">
                    <label className="admin-field">
                      <span>Prompt version</span>
                      <select value={targetVersion} onChange={(event) => setTargetVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`eval-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Judge</span>
                      <select value={goldenJudge} onChange={(event) => setGoldenJudge(event.currentTarget.value)}>
                        <option value="heuristic">Heuristic</option>
                        <option value="llm">LLM</option>
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Golden set</span>
                      <select value={`${goldenSetID}::${goldenSetVersion}`} onChange={(event) => {
                        const [id, version] = event.currentTarget.value.split("::");
                        setGoldenSetID(id || "");
                        setGoldenSetVersion(version || "");
                      }}>
                        <option value="">Select set</option>
                        {goldenSets.map((set) => (
                          <option key={`${set.id}::${set.version || ""}`} value={`${set.id}::${set.version || ""}`}>
                            {set.id} · {set.version || "latest"} · {set.cases.length} cases
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Eval run</span>
                      <Input value={evalRunID} onChange={(event) => setEvalRunID(event.currentTarget.value)} placeholder="auto after eval" />
                    </label>
                  </div>
                  <div className="admin-action-row">
                    <Button className="primary skill-action" onClick={runPromptEval} disabled={busy === "eval" || !targetVersion || !selectedGoldenSet}>
                      <ShieldCheck size={16} />
                      <span>{busy === "eval" ? "Running" : "Run eval"}</span>
                    </Button>
                  </div>
                  {evalRun && (
                    <div className="prompt-comparison-grid">
                      <AdminMetric label="Pass rate" value={formatPercent(evalRun.summary.pass_rate)} />
                      <AdminMetric label="Passed" value={String(evalRun.run.passed)} />
                      <AdminMetric label="Failed" value={String(evalRun.run.failed)} />
                      <AdminMetric label="Warnings" value={String(evalRun.run.warning)} />
                    </div>
                  )}
                </section>
              )}
              {tab === "experiments" && (
                <section className="admin-card wide">
                  <div className="admin-card-head">
                    <h3>Experiment</h3>
                    <Button className="small ghost" onClick={() => loadPromptExperiments()} disabled={busy === "experiments"}>
                      <RefreshCw size={14} />
                      <span>Refresh</span>
                    </Button>
                  </div>
                  <div className="golden-form-grid">
                    <label className="admin-field">
                      <span>ID</span>
                      <Input value={experimentID} onChange={(event) => setExperimentID(event.currentTarget.value)} placeholder="optional" />
                    </label>
                    <label className="admin-field">
                      <span>Name</span>
                      <Input value={experimentName} onChange={(event) => setExperimentName(event.currentTarget.value)} placeholder="optional" />
                    </label>
                    <label className="admin-field">
                      <span>Control</span>
                      <select value={experimentControlVersion} onChange={(event) => setExperimentControlVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`control-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Candidate</span>
                      <select value={experimentCandidateVersion} onChange={(event) => setExperimentCandidateVersion(event.currentTarget.value)}>
                        <option value="">Select version</option>
                        {versions.map((version) => <option key={`experiment-candidate-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                    <label className="admin-field">
                      <span>Candidate weight</span>
                      <Input type="number" min={1} max={99} value={String(experimentWeight)} onChange={(event) => setExperimentWeight(Number(event.currentTarget.value))} />
                    </label>
                  </div>
                  <div className="admin-action-row">
                    <Button className="primary skill-action" onClick={createExperiment} disabled={busy === "experiment-create"}>
                      <Beaker size={16} />
                      <span>Save experiment</span>
                    </Button>
                    <select value={selectedExperimentID} onChange={(event) => setSelectedExperimentID(event.currentTarget.value)}>
                      <option value="">Select experiment</option>
                      {experiments.map((experiment) => <option key={experiment.id} value={experiment.id}>{experiment.id} · {experiment.status}</option>)}
                    </select>
                    <Button className="skill-action" onClick={() => updateExperiment("start")} disabled={!selectedExperimentID || busy === "experiment-start"}>Start</Button>
                    <Button className="skill-action" onClick={() => updateExperiment("pause")} disabled={!selectedExperimentID || busy === "experiment-pause"}>Pause</Button>
                    <Button className="skill-action" onClick={() => updateExperiment("complete")} disabled={!selectedExperimentID || busy === "experiment-complete"}>Complete</Button>
                  </div>
                  <div className="admin-table">
                    {experiments.map((experiment) => (
                      <div key={experiment.id} className="admin-table-row">
                        <StatusBadge value={experiment.status} />
                        <span>
                          <strong>{experiment.name || experiment.id}</strong>
                          <small>{experiment.id} · {experiment.winner_variant_id || "no winner"}</small>
                        </span>
                        <small>{formatTime(experiment.updated_at || experiment.created_at || "")}</small>
                      </div>
                    ))}
                    {!experiments.length && <p className="muted-text">No experiments for this prompt.</p>}
                  </div>
                </section>
              )}
              {tab === "usage" && (
                <section className="admin-card wide">
                  <div className="admin-card-head">
                    <h3>Usage / trace</h3>
                    <Button className="small ghost" onClick={() => loadUsage()} disabled={busy === "usage"}>
                      <RefreshCw size={14} />
                      <span>Refresh</span>
                    </Button>
                  </div>
                  <div className="golden-form-grid">
                    <label className="admin-field">
                      <span>Version</span>
                      <select value={targetVersion} onChange={(event) => setTargetVersion(event.currentTarget.value)}>
                        <option value="">All versions</option>
                        {versions.map((version) => <option key={`usage-${version.version}`} value={version.version}>{versionLabel(version)}</option>)}
                      </select>
                    </label>
                  </div>
                  <div className="prompt-comparison-grid">
                    <AdminMetric label="Requests" value={String(usage?.requests || 0)} />
                    <AdminMetric label="Failures" value={String(usage?.failures || 0)} />
                    <AdminMetric label="Tokens" value={formatNumber(usage?.total_tokens || 0)} />
                    <AdminMetric label="Cost" value={formatUSD(usage?.estimated_cost_usd || 0)} />
                  </div>
                  <div className="admin-table">
                    {(usage?.recent || []).slice(0, 16).map((record) => (
                      <div key={record.id} className="admin-table-row">
                        <StatusBadge value={record.status} />
                        <span>
                          <strong>{record.provider} / {record.model}</strong>
                          <small>{record.prompt_id}@{record.prompt_version || "unknown"} · {formatNumber(record.total_tokens)} tokens</small>
                        </span>
                        <small>{formatTime(record.created_at)}</small>
                      </div>
                    ))}
                    {!usage?.recent?.length && <p className="muted-text">No LLM usage for this filter.</p>}
                  </div>
                  <div className="admin-table">
                    {evalResults.slice(0, 12).map((result) => (
                      <div key={result.id} className="admin-table-row">
                        <StatusBadge value={result.status} />
                        <span>
                          <strong>{result.subject_type}:{result.subject_id}</strong>
                          <small>{result.run_id} · {result.prompt_id}@{result.prompt_version || "unknown"}</small>
                        </span>
                        <small>{formatNumber(Math.round((result.score || 0) * 100))}</small>
                      </div>
                    ))}
                    {!evalResults.length && <p className="muted-text">No eval results for this filter.</p>}
                  </div>
                </section>
              )}
            </div>
          </>
        )}
      </AdminDetailPanel>
    </AdminSplitPane>
  );
}
