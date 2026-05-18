import { FormEvent, forwardRef, ReactNode, RefObject, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AlertCircle,
  Archive,
  Brain,
  Briefcase,
  ChevronDown,
  Clock,
  Database,
  Download,
  FileText,
  FileUp,
  Image,
  Info,
  LogOut,
  Menu,
  MessageCircle,
  MessageSquarePlus,
  Mic,
  MicOff,
  PanelLeft,
  PlayCircle,
  RefreshCw,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Sparkles,
  Square,
  Star,
  Trash2,
  UserX,
  Volume2,
  VolumeX,
  X
} from "lucide-react";
import { ApiClient, ApiError } from "./api/client";
import type { AdminHealthStatus, AdminSkill, AdminUser, Asset, AuditLogRecord, AuditLogSummary, AuthSession, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunSummary, EvaluationThresholds, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, MemoryItem, MemoryMaintenanceAction, MemorySettings, Message, MessageSearchResult, PersonalizationSettings, ReadinessStatus, RiskEvent, RiskReviewSummary, RiskSummary, RuntimeEvent, Session, Skill, SkillExecution, SkillExecutionSummary, SkillPolicyConfig, SkillReviewResult, SkillVersion } from "./types";
import { readSSEStream } from "./lib/sse";
import { sessionTitle } from "./lib/sessionTitle";

type Status = {
  tone: "idle" | "ok" | "busy" | "error";
  text: string;
};

const defaultMemorySettings: MemorySettings = {
  enabled: true,
  capture_enabled: true,
  context_enabled: true,
  updated_at: ""
};

const defaultPersonalizationSettings: PersonalizationSettings = {
  profile: {
    nickname: "",
    occupation: "",
    about: ""
  },
  style: {
    preset: "default",
    tone: "default"
  },
  traits: {
    warmth: "default",
    enthusiasm: "default",
    headings_and_lists: "default",
    emoji: "default"
  },
  custom_instructions: "",
  feature_flags: {
    quick_answers: true,
    use_saved_memory: true,
    use_chat_history: true,
    use_browser_memory: false
  },
  version: 1,
  updated_at: ""
};

const brandLogoSrc = "/logo.png";

function BrandLogo({ className = "brand-mark" }: { className?: string }) {
  return (
    <span className={className} aria-hidden="true">
      <img src={brandLogoSrc} alt="" />
    </span>
  );
}

function applyMemorySettingsPatch(
  current: MemorySettings,
  patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>
): MemorySettings {
  const next = { ...current, updated_at: new Date().toISOString() };
  if (patch.enabled !== undefined) {
    next.capture_enabled = patch.enabled;
    next.context_enabled = patch.enabled;
  }
  if (patch.capture_enabled !== undefined) next.capture_enabled = patch.capture_enabled;
  if (patch.context_enabled !== undefined) next.context_enabled = patch.context_enabled;
  next.enabled = next.capture_enabled || next.context_enabled;
  return next;
}

type ServiceStatus = Status & {
  details?: string;
};

type RightPanelTab = "skills" | "jobs" | "attachments" | "artifacts";
type RightPanelSearch = Record<RightPanelTab, string>;
type JobStreamStatus = "idle" | "connecting" | "live" | "reconnecting" | "failed";
type AdminSection = "skills" | "users" | "jobs-assets" | "health-cost" | "audit" | "evaluation";
type AdminTabOption<T extends string> = {
  id: T;
  label: string;
  description?: string;
  icon?: ReactNode;
  count?: number;
};

type ConfirmDialog = {
  title: string;
  message: string;
  detail?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
};

const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);
const terminalRuntimeEvents = new Set(["done", "error", "cancelled"]);
const serviceStatusPollMs = 10_000;
const activeJobStorageKey = "agentapi.activeJob";
const recentSkillsStorageKey = "agentapi.recentSkills";
const adminTokenStorageKey = "agentapi.adminToken";
const jobReconnectBaseMs = 1_000;
const jobReconnectMaxMs = 10_000;

function isAdminPath(): boolean {
  return typeof window !== "undefined" && window.location.pathname.replace(/\/+$/, "") === "/admin";
}

export function App() {
  const [auth, setAuth] = useState<AuthSession | null>(null);
  const api = useMemo(() => new ApiClient(setAuth), []);
  const [status, setStatus] = useState<Status>({ tone: "idle", text: "Idle" });
  const [serviceStatus, setServiceStatus] = useState<ServiceStatus>({ tone: "busy", text: "Checking" });
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionId, setSessionId] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [online, setOnline] = useState(() => typeof navigator === "undefined" ? true : navigator.onLine);
  const [draft, setDraft] = useState("");
  const [assistantDraft, setAssistantDraft] = useState("");
  const [runtimeError, setRuntimeError] = useState("");
  const [skills, setSkills] = useState<Skill[]>([]);
  const [skillDetail, setSkillDetail] = useState<Skill | null>(null);
  const [recentSkillNames, setRecentSkillNames] = useState<string[]>(readRecentSkills);
  const [adminToken, setAdminToken] = useState(readAdminToken);
  const [adminView, setAdminView] = useState(isAdminPath);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedJobId, setSelectedJobId] = useState("");
  const [autoExpandedJobId, setAutoExpandedJobId] = useState("");
  const [jobStreamStatus, setJobStreamStatus] = useState<JobStreamStatus>("idle");
  const [jobStreamNotice, setJobStreamNotice] = useState("");
  const [jobEvents, setJobEvents] = useState<JobEvent[]>([]);
  const [attachments, setAttachments] = useState<Asset[]>([]);
  const [artifacts, setArtifacts] = useState<Asset[]>([]);
  const [globalSearchOpen, setGlobalSearchOpen] = useState(false);
  const [globalSearchQuery, setGlobalSearchQuery] = useState("");
  const [globalSearchResults, setGlobalSearchResults] = useState<MessageSearchResult[]>([]);
  const [globalSearchLoading, setGlobalSearchLoading] = useState(false);
  const [globalSearchError, setGlobalSearchError] = useState("");
  const [globalSearchTarget, setGlobalSearchTarget] = useState<{ sessionID: string; messageIndex: number } | null>(null);
  const [highlightedMessageIndex, setHighlightedMessageIndex] = useState<number | null>(null);
  const [mobileNav, setMobileNav] = useState(false);
  const [busyChat, setBusyChat] = useState(false);
  const [inputMode, setInputMode] = useState<"text" | "live">("text");
  const [liveStatus, setLiveStatus] = useState<"idle" | "connecting" | "listening" | "paused" | "error">("idle");
  const [liveMuted, setLiveMuted] = useState(false);
  const [liveUserDraft, setLiveUserDraft] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadError, setUploadError] = useState("");
  const [pendingAttachments, setPendingAttachments] = useState<Asset[]>([]);
  const [previewAsset, setPreviewAsset] = useState<{ asset: Asset; url: string } | null>(null);
  const [assetMemoryBusy, setAssetMemoryBusy] = useState<Record<string, boolean>>({});
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsModalOpen, setSettingsModalOpen] = useState(false);
  const [memoryManagerOpen, setMemoryManagerOpen] = useState(false);
  const [memoryScope, setMemoryScope] = useState<"all" | "session">("all");
  const [memoryStatusFilter, setMemoryStatusFilter] = useState("active");
  const [memoryLevelFilter, setMemoryLevelFilter] = useState("all");
  const [memoryItems, setMemoryItems] = useState<MemoryItem[]>([]);
  const [memoryActions, setMemoryActions] = useState<MemoryMaintenanceAction[]>([]);
  const [memoryLoading, setMemoryLoading] = useState(false);
  const [memoryError, setMemoryError] = useState("");
  const [memorySettings, setMemorySettings] = useState<MemorySettings>(defaultMemorySettings);
  const [personalizationSettings, setPersonalizationSettings] = useState<PersonalizationSettings>(defaultPersonalizationSettings);
  const [personalizationSaving, setPersonalizationSaving] = useState(false);
  const [leftSidebarOpen, setLeftSidebarOpen] = useState(true);
  const [rightPanelOpen, setRightPanelOpen] = useState(true);
  const [rightPanelTab, setRightPanelTab] = useState<RightPanelTab>("skills");
  const [rightPanelSearch, setRightPanelSearch] = useState<RightPanelSearch>({
    skills: "",
    jobs: "",
    attachments: "",
    artifacts: ""
  });
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialog | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const liveSocketRef = useRef<WebSocket | null>(null);
  const liveMediaRef = useRef<MediaStream | null>(null);
  const liveAudioContextRef = useRef<AudioContext | null>(null);
  const liveProcessorRef = useRef<ScriptProcessorNode | null>(null);
  const liveSourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const livePlaybackContextRef = useRef<AudioContext | null>(null);
  const livePlaybackTimeRef = useRef(0);
  const livePlaybackGenerationRef = useRef(0);
  const livePlaybackSourcesRef = useRef<Set<AudioBufferSourceNode>>(new Set());
  const liveMutedRef = useRef(liveMuted);
  const liveAudioChunkCountRef = useRef(0);
  const livePlaybackQueueRef = useRef(Promise.resolve());
  const attachmentInputRef = useRef<HTMLInputElement | null>(null);
  const jobSourceRef = useRef<EventSource | null>(null);
  const jobReconnectTimerRef = useRef<number | null>(null);
  const jobReconnectAttemptRef = useRef(0);
  const jobStreamClosedRef = useRef(false);
  const activeJobStreamIdRef = useRef("");
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const accountRef = useRef<HTMLDivElement | null>(null);
  const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const lastJobEventRef = useRef("");
  const confirmResolverRef = useRef<((confirmed: boolean) => void) | null>(null);
  const artifactsRef = useRef<Asset[]>([]);
  const globalSearchDialogRef = useFocusTrap<HTMLElement>(globalSearchOpen, () => setGlobalSearchOpen(false));
  const skillDetailDialogRef = useFocusTrap<HTMLElement>(Boolean(skillDetail), () => setSkillDetail(null));
  const authSession = auth || api.session();
  const activeSession = sessions.find((item) => item.id === sessionId);
  const latestJob = jobs[0];
  const latestJobId = latestJob?.id || "";
  const rightSearch = rightPanelSearch[rightPanelTab];
  const filteredSkills = useMemo(
    () => skills.filter((skill) => fuzzyMatch(rightPanelSearch.skills, [
      skill.name,
      skill.display_name,
      skill.description,
      skill.short_description,
      skill.category,
      skill.version,
      ...(skill.tags || [])
    ])),
    [skills, rightPanelSearch.skills]
  );
  const filteredJobs = useMemo(
    () => jobs.filter((job) => fuzzyMatch(rightPanelSearch.jobs, [job.id, job.content])),
    [jobs, rightPanelSearch.jobs]
  );
  const filteredAttachments = useMemo(
    () => attachments.filter((asset) => fuzzyMatch(rightPanelSearch.attachments, [asset.filename, asset.id])),
    [attachments, rightPanelSearch.attachments]
  );
  const filteredArtifacts = useMemo(
    () => artifacts.filter((asset) => fuzzyMatch(rightPanelSearch.artifacts, [asset.filename, asset.id])),
    [artifacts, rightPanelSearch.artifacts]
  );
  const recoveryBanner = !online
    ? { tone: "error", text: "Network connection lost. New messages may fail until the browser is back online." }
    : selectedJobId && (jobStreamStatus === "reconnecting" || jobStreamStatus === "failed")
      ? { tone: jobStreamStatus === "failed" ? "error" : "busy", text: jobStreamNotice || "Restoring live job updates..." }
      : null;

  useEffect(() => {
    const existing = api.session();
    if (existing) {
      setAuth(existing);
      bootstrap(api, setStatus, setSessions, setSessionId, setMessages, setSkills, setJobs, setAttachments, setArtifacts).catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          setAuth(null);
          setStatus({ tone: "idle", text: "Please log in again" });
          return;
        }
        setStatus({ tone: "error", text: errorMessage(err) });
      });
      loadMemorySettings().catch(() => {});
      loadPersonalizationSettings().catch(() => {});
    }
    return () => {
      closeJobStream();
      stopLiveMode(false);
    };
  }, [api]);

  useEffect(() => {
    const onPopState = () => setAdminView(isAdminPath());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      const next = await readServiceStatus(api);
      if (!cancelled) setServiceStatus(next);
    };
    const refreshWhenVisible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    refresh();
    const id = window.setInterval(refresh, serviceStatusPollMs);
    window.addEventListener("focus", refresh);
    window.addEventListener("online", refresh);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    return () => {
      cancelled = true;
      window.clearInterval(id);
      window.removeEventListener("focus", refresh);
      window.removeEventListener("online", refresh);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
    };
  }, [api]);

  useEffect(() => {
    const markOffline = () => {
      setOnline(false);
      setServiceStatus({ tone: "error", text: "Offline", details: "Network connection is unavailable" });
    };
    const markOnline = () => {
      setOnline(true);
      readServiceStatus(api).then(setServiceStatus).catch(() => {});
      if (sessionId) refreshSessionData(sessionId).catch(() => {});
      if (selectedJobId) openJobStream(selectedJobId);
    };
    window.addEventListener("offline", markOffline);
    window.addEventListener("online", markOnline);
    return () => {
      window.removeEventListener("offline", markOffline);
      window.removeEventListener("online", markOnline);
    };
  }, [api, selectedJobId, sessionId]);

  useEffect(() => {
    if (!sessionId) return;
    refreshSessionData(sessionId).catch((error) => showError(error));
    stopLiveMode(false);
  }, [sessionId]);

  useEffect(() => {
    artifactsRef.current = artifacts;
  }, [artifacts]);

  useEffect(() => {
    resizeComposerInput(composerInputRef.current);
  }, [draft]);

  useEffect(() => {
    liveMutedRef.current = liveMuted;
    if (liveMuted) {
      stopLivePlayback();
    }
  }, [liveMuted]);

  useEffect(() => {
    if (!selectedJobId) {
      closeJobStream();
      setJobEvents([]);
      setJobStreamStatus("idle");
      setJobStreamNotice("");
      return;
    }
    openJobStream(selectedJobId);
    return () => closeJobStream();
  }, [selectedJobId]);

  useEffect(() => {
    if (!latestJobId || autoExpandedJobId === latestJobId) return;
    setSelectedJobId(latestJobId);
    setAutoExpandedJobId(latestJobId);
    if (latestJob && !terminalJobs.has(latestJob.status) && (!sessionId || latestJob.session_id === sessionId)) {
      setRightPanelOpen(true);
      setRightPanelTab("jobs");
      setStatus({ tone: "busy", text: "Restoring job" });
    }
  }, [autoExpandedJobId, latestJob, latestJobId, sessionId]);

  useEffect(() => {
    if (!selectedJobId) return;
    const job = jobs.find((item) => item.id === selectedJobId);
    if (!job) return;
    if (terminalJobs.has(job.status)) {
      clearActiveJob(selectedJobId);
      return;
    }
    saveActiveJob(job.id, job.session_id || sessionId);
  }, [jobs, selectedJobId, sessionId]);

  useEffect(() => {
    const node = messagesRef.current;
    if (!node) return;
    node.scrollTop = node.scrollHeight;
  }, [messages, assistantDraft, liveUserDraft, sessionId]);

  useEffect(() => {
    if (!globalSearchOpen) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") setGlobalSearchOpen(false);
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [globalSearchOpen]);

  useEffect(() => {
    if (!globalSearchOpen) return;
    const query = globalSearchQuery.trim();
    setGlobalSearchError("");
    if (!query) {
      setGlobalSearchResults([]);
      setGlobalSearchLoading(false);
      return;
    }
    let cancelled = false;
    setGlobalSearchLoading(true);
    const id = window.setTimeout(() => {
      api.searchMessages(query)
        .then((items) => {
          if (!cancelled) setGlobalSearchResults(items);
        })
        .catch((error) => {
          if (!cancelled) {
            setGlobalSearchResults([]);
            setGlobalSearchError(errorMessage(error));
            readServiceStatus(api).then(setServiceStatus).catch(() => {});
          }
        })
        .finally(() => {
          if (!cancelled) setGlobalSearchLoading(false);
        });
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(id);
    };
  }, [api, globalSearchOpen, globalSearchQuery]);

  useEffect(() => {
    if (!globalSearchTarget || globalSearchTarget.sessionID !== sessionId) return;
    const id = window.requestAnimationFrame(() => {
      const target = messagesRef.current?.querySelector<HTMLElement>(`[data-message-index="${globalSearchTarget.messageIndex}"]`);
      if (!target) return;
      target.scrollIntoView({ block: "center", behavior: "smooth" });
      setHighlightedMessageIndex(globalSearchTarget.messageIndex);
      window.setTimeout(() => setHighlightedMessageIndex(null), 1600);
      setGlobalSearchTarget(null);
    });
    return () => window.cancelAnimationFrame(id);
  }, [messages, sessionId, globalSearchTarget]);

  useEffect(() => {
    if (!settingsOpen) return;
    const close = (event: MouseEvent) => {
      const target = event.target;
      if (target instanceof Node && accountRef.current?.contains(target)) return;
      setSettingsOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [settingsOpen]);

  async function refreshAll() {
    setServiceStatus(await readServiceStatus(api));
    await bootstrap(api, setStatus, setSessions, setSessionId, setMessages, setSkills, setJobs, setAttachments, setArtifacts);
    await loadMemorySettings();
    await loadPersonalizationSettings();
  }

  async function refreshSessionData(id: string, options: { revealNewArtifacts?: boolean } = {}) {
    setStatus({ tone: "busy", text: "Refreshing" });
    const previousArtifactIds = new Set(artifactsRef.current.map((asset) => asset.id));
    const [session, jobList, attachmentList, artifactList] = await Promise.all([
      api.getSession(id),
      api.jobs(id),
      api.attachments(id),
      api.artifacts(id)
    ]);
    setMessages(visibleMessages(session.messages || []));
    setSessions((current) => upsertSession(current, session));
    setJobs(jobList);
    setAttachments(attachmentList);
    artifactsRef.current = artifactList;
    setArtifacts(artifactList);
    if (options.revealNewArtifacts && artifactList.some((asset) => !previousArtifactIds.has(asset.id))) {
      revealRightPanel("artifacts");
    }
    setStatus({ tone: "ok", text: "Ready" });
  }

  function revealRightPanel(tab: RightPanelTab) {
    setRightPanelOpen(true);
    setRightPanelTab(tab);
  }

  async function openSearchResult(result: MessageSearchResult) {
    setGlobalSearchTarget({ sessionID: result.session_id, messageIndex: result.message_index });
    setSessionId(result.session_id);
    setMobileNav(false);
    setGlobalSearchOpen(false);
    if (result.session_id === sessionId) {
      await refreshSessionData(result.session_id);
    }
  }

  async function submitAuth(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const email = String(form.get("email") || "").trim();
    const password = String(form.get("password") || "");
    const confirmPassword = String(form.get("confirmPassword") || "");
    const displayName = String(form.get("displayName") || "").trim();
    if (authMode === "register" && password !== confirmPassword) {
      setStatus({ tone: "error", text: "Passwords do not match" });
      return;
    }
    setStatus({ tone: "busy", text: authMode === "register" ? "Creating account and sending verification email" : "Signing in" });
    try {
      if (authMode === "register") {
        const result = await api.register(email, password, displayName);
        if ("verification_required" in result && result.verification_required) {
          setAuthMode("login");
          setStatus({ tone: "ok", text: `Verification email sent to ${result.email}. Check your inbox and spam folder.` });
          return;
        }
      } else {
        await api.login(email, password);
      }
      await refreshAll();
    } catch (error) {
      if (authMode === "register") {
        const message = `Could not send verification email: ${errorMessage(error)}`;
        setRuntimeError(message);
        setStatus({ tone: "error", text: message });
        readServiceStatus(api).then(setServiceStatus).catch(() => {});
      } else {
        showError(error);
      }
    }
  }

  async function createSession() {
    try {
      const session = await api.createSession();
      setSessions((current) => [session, ...current]);
      setSessionId(session.id);
      setMessages([]);
    } catch (error) {
      showError(error);
    }
  }

  async function removeSession(targetSessionId: string) {
    if (!targetSessionId) return;
    const targetSession = sessions.find((item) => item.id === targetSessionId);
    const targetTitle = targetSession ? sessionTitle(targetSession) : targetSessionId;
    const confirmed = await requestConfirmation({
      title: "Delete session?",
      message: `This will delete "${targetTitle}" and its related data.`,
      detail: "Messages, session memory, jobs, attachments, and artifacts linked to this session may no longer be available.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      await api.deleteSession(targetSessionId);
      const next = sessions.filter((item) => item.id !== targetSessionId);
      setSessions(next);
      if (targetSessionId === sessionId) {
        setSessionId(next[0]?.id || "");
        setMessages(visibleMessages(next[0]?.messages || []));
      }
      setSettingsOpen(false);
    } catch (error) {
      showError(error);
    }
  }

  async function exportData() {
    try {
      const payload = await api.exportData();
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "agent-data-export.json";
      link.click();
      URL.revokeObjectURL(url);
      setSettingsOpen(false);
      setStatus({ tone: "ok", text: "Exported" });
    } catch (error) {
      showError(error);
    }
  }

  async function deleteSessionMemory() {
    if (!sessionId) return;
    const confirmed = await requestConfirmation({
      title: "Delete session memory?",
      message: "This will remove memory saved for the current session.",
      detail: "The chat messages remain, but future responses will not use this session memory.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      await api.deleteSessionMemory(sessionId);
      setSettingsOpen(false);
      setStatus({ tone: "ok", text: "Session memory deleted" });
    } catch (error) {
      showError(error);
    }
  }

  async function deleteAllMemory() {
    const confirmed = await requestConfirmation({
      title: "Delete all memory?",
      message: "This will remove all saved memory for your account.",
      detail: "Sessions and messages remain, but personalized memory will be cleared.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      await api.deleteAllMemory();
      setSettingsOpen(false);
      setStatus({ tone: "ok", text: "Memory deleted" });
    } catch (error) {
      showError(error);
    }
  }

  async function loadMemorySettings() {
    if (!api.session()) return;
    setMemorySettings(await api.memorySettings());
  }

  async function loadPersonalizationSettings() {
    if (!api.session()) return;
    setPersonalizationSettings(await api.personalization());
  }

  async function updateMemorySettings(patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) {
    const previous = memorySettings;
    const optimistic = applyMemorySettingsPatch(previous, patch);
    setMemorySettings(optimistic);
    setStatus({ tone: "busy", text: "Saving memory settings" });
    try {
      const next = await api.updateMemorySettings(patch);
      setMemorySettings(next);
      setStatus({ tone: "ok", text: next.enabled ? "Memory settings saved" : "Memory disabled" });
    } catch (error) {
      setMemorySettings(previous);
      showError(error);
    }
  }

  async function updatePersonalizationSettings(patch: Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">>) {
    const previous = personalizationSettings;
    setPersonalizationSaving(true);
    setStatus({ tone: "busy", text: "Saving personalization" });
    try {
      const next = await api.updatePersonalization(patch);
      setPersonalizationSettings(next);
      setStatus({ tone: "ok", text: "Personalization saved" });
    } catch (error) {
      setPersonalizationSettings(previous);
      showError(error);
    } finally {
      setPersonalizationSaving(false);
    }
  }

  async function resetPersonalizationSettings() {
    const confirmed = await requestConfirmation({
      title: "Reset personalization?",
      message: "This will clear your explicit style, profile, and custom instruction settings.",
      detail: "Saved memory is not deleted.",
      confirmLabel: "Reset",
      danger: false
    });
    if (!confirmed) return;
    setPersonalizationSaving(true);
    setStatus({ tone: "busy", text: "Resetting personalization" });
    try {
      const next = await api.resetPersonalization();
      setPersonalizationSettings(next);
      setStatus({ tone: "ok", text: "Personalization reset" });
    } catch (error) {
      showError(error);
    } finally {
      setPersonalizationSaving(false);
    }
  }

  function updateAdminToken(next: string) {
    setAdminToken(next);
    writeAdminToken(next);
  }

  async function openMemoryManager(scope: "all" | "session" = "all") {
    setSettingsOpen(false);
    setSettingsModalOpen(false);
    setMemoryManagerOpen(true);
    setMemoryScope(scope);
    await loadMemoryItems(scope);
    await loadMemoryMaintenance();
  }

  async function loadMemoryItems(scope: "all" | "session" = memoryScope, status = memoryStatusFilter, level = memoryLevelFilter) {
    if (scope === "session" && !sessionId) {
      setMemoryItems([]);
      return;
    }
    setMemoryLoading(true);
    setMemoryError("");
    try {
      const items = await api.memoryItems({
        sessionId: scope === "session" ? sessionId : undefined,
        status,
        level
      });
      setMemoryItems(items);
    } catch (error) {
      setMemoryError(error instanceof Error ? error.message : "Unable to load memory");
    } finally {
      setMemoryLoading(false);
    }
  }

  async function loadMemoryMaintenance() {
    try {
      setMemoryActions(await api.memoryMaintenance());
    } catch {
      setMemoryActions([]);
    }
  }

  async function deleteMemoryItem(item: MemoryItem) {
    const confirmed = await requestConfirmation({
      title: "Delete memory item?",
      message: "This will remove this saved memory item.",
      detail: "Chat messages remain, but this item will no longer be shown or used as memory.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      await api.deleteMemoryItem(item.id);
      setMemoryItems((items) => items.filter((candidate) => candidate.id !== item.id));
      setStatus({ tone: "ok", text: "Memory item deleted" });
    } catch (error) {
      showError(error);
    }
  }

  async function updateMemoryItem(item: MemoryItem, patch: Partial<Pick<MemoryItem, "content" | "namespace" | "category" | "tags" | "visibility">>) {
    try {
      const updated = await api.updateMemoryItem(item.id, patch);
      setMemoryItems((items) => items.map((candidate) => candidate.id === updated.id ? updated : candidate));
      setStatus({ tone: "ok", text: "Memory updated" });
    } catch (error) {
      showError(error);
    }
  }

  async function sendMemoryFeedback(item: MemoryItem, type: "important" | "incorrect" | "not_relevant") {
    try {
      const updated = await api.memoryFeedback(item.id, type);
      setMemoryItems((items) => updated.status === "active"
        ? items.map((candidate) => candidate.id === updated.id ? updated : candidate)
        : items.filter((candidate) => candidate.id !== updated.id)
      );
      setStatus({ tone: "ok", text: "Memory feedback saved" });
    } catch (error) {
      showError(error);
    }
  }

  async function resolveMemoryConflict(item: MemoryItem, action: "accept" | "reject" | "keep_both") {
    try {
      const updated = await api.memoryResolve(item.id, action);
      setMemoryItems((items) => items.map((candidate) => candidate.id === updated.id ? updated : candidate));
      setStatus({ tone: "ok", text: "Memory conflict resolved" });
      void loadMemoryItems(memoryScope);
    } catch (error) {
      showError(error);
    }
  }

  async function rebuildMemoryAbstractions() {
    try {
      setMemoryLoading(true);
      const rebuilt = await api.rebuildMemory();
      setStatus({ tone: "ok", text: `Memory rebuilt (${rebuilt.length})` });
      await loadMemoryItems(memoryScope);
      await loadMemoryMaintenance();
    } catch (error) {
      showError(error);
    } finally {
      setMemoryLoading(false);
    }
  }

  async function scoreMemoryQuality() {
    try {
      setMemoryLoading(true);
      const scored = await api.scoreMemory();
      setStatus({ tone: "ok", text: `Memory scored (${scored.length})` });
      await loadMemoryItems(memoryScope);
      await loadMemoryMaintenance();
    } catch (error) {
      showError(error);
    } finally {
      setMemoryLoading(false);
    }
  }

  async function runMemoryMaintenance() {
    try {
      setMemoryLoading(true);
      const report = await api.runMemoryMaintenance();
      setMemoryActions(report.actions);
      setStatus({ tone: "ok", text: `Memory organized (${report.applied.length} applied, ${report.actions.length} pending review)` });
      await loadMemoryItems(memoryScope);
      await loadMemoryMaintenance();
    } catch (error) {
      showError(error);
    } finally {
      setMemoryLoading(false);
    }
  }

  async function deleteAccount() {
    const confirmed = await requestConfirmation({
      title: "Delete account?",
      message: "This will delete this account and all sessions, memory, and workspace data.",
      detail: "This action cannot be undone.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      await api.deleteAccount();
      setSettingsOpen(false);
    } catch (error) {
      showError(error);
    }
  }

  async function logout() {
    try {
      await api.logout();
      setSettingsOpen(false);
    } catch (error) {
      showError(error);
    }
  }

  async function sendMessage() {
    const content = draft.trim();
    if ((!content && pendingAttachments.length === 0) || !sessionId || busyChat) return;
    const attachmentIds = pendingAttachments.map((asset) => asset.id);
    const abort = new AbortController();
    let routedToJob = false;
    let sawRuntimeError = false;
    abortRef.current = abort;
    setDraft("");
    setAssistantDraft("");
    setRuntimeError("");
    const displayContent = content || "Please analyze the attached file(s).";
    setMessages((current) => appendRuntimeMessage(current, { role: "user", content: messageWithAttachmentNames(displayContent, pendingAttachments), created_at: new Date().toISOString() }));
    setBusyChat(true);
    setStatus({ tone: "busy", text: "Generating" });
    try {
      const response = await api.chatResponse(sessionId, displayContent, attachmentIds, abort.signal);
      await readSSEStream(response, ({ data }) => {
        if (data.type === "job") routedToJob = true;
        if (data.type === "error") sawRuntimeError = true;
        handleRuntimeEvent(data);
      });
      setPendingAttachments([]);
      if (!routedToJob) await refreshSessionData(sessionId, { revealNewArtifacts: true });
      if (sawRuntimeError) {
        setStatus((current) => current.tone === "error" ? current : { tone: "error", text: "Request failed" });
      }
    } catch (error) {
      if ((error as Error).name !== "AbortError") {
        const message = errorMessage(error);
        setRuntimeError(`Message delivery failed. Your sent message was kept in the conversation. ${message}`);
        setStatus({ tone: "error", text: "Message failed" });
        setMessages((current) => appendRuntimeMessage(current, {
          role: "assistant",
          content: `Message delivery failed. Your sent message was kept here, but the response stream did not finish.\n\n${message}`,
          created_at: new Date().toISOString()
        }));
        readServiceStatus(api).then(setServiceStatus).catch(() => {});
      }
    } finally {
      abortRef.current = null;
      setBusyChat(false);
      setAssistantDraft("");
    }
  }

  async function cancelChat() {
    abortRef.current?.abort();
    if (sessionId) {
      try {
        await api.cancelSession(sessionId);
      } catch {
        // The server may have already completed the request.
      }
    }
    setBusyChat(false);
    setStatus({ tone: "idle", text: "Cancelled" });
  }

  async function startLiveMode() {
    if (!sessionId || liveStatus !== "idle") return;
    if (typeof WebSocket === "undefined" || !navigator.mediaDevices?.getUserMedia) {
      setRuntimeError("Live voice is unavailable in this browser.");
      setLiveStatus("error");
      return;
    }
    stopLiveMode(false);
    setInputMode("live");
    setRuntimeError("");
    setAssistantDraft("");
    setLiveUserDraft("");
    setLiveMuted(false);
    liveAudioChunkCountRef.current = 0;
    stopLivePlayback();
    try {
      await ensureLivePlaybackContext();
    } catch {
      setRuntimeError("Audio playback is unavailable in this browser.");
      setLiveStatus("error");
      return;
    }
    setLiveStatus("connecting");
    setStatus({ tone: "busy", text: "Connecting live voice" });
    const socket = new WebSocket(api.liveSessionURL(sessionId));
    liveSocketRef.current = socket;
    socket.onmessage = (message) => {
      try {
        void handleLiveRuntimeEvent(JSON.parse(message.data) as RuntimeEvent, socket);
      } catch {
        // Ignore malformed live frames; the socket error handler covers transport failures.
      }
    };
    socket.onerror = () => {
      if (liveSocketRef.current !== socket) return;
      setLiveStatus("error");
      setStatus({ tone: "error", text: "Live voice failed" });
    };
    socket.onclose = () => {
      if (liveSocketRef.current !== socket) return;
      cleanupLiveAudio();
      liveSocketRef.current = null;
      setLiveStatus("idle");
      setStatus((current) => current.tone === "error" ? current : { tone: "idle", text: "Live voice stopped" });
    };
  }

  function stopLiveMode(sendEnd = true) {
    const socket = liveSocketRef.current;
    cleanupLiveAudio();
    liveSocketRef.current = null;
    if (socket && socket.readyState === WebSocket.OPEN) {
      if (sendEnd) {
        socket.send(JSON.stringify({ type: "audio_end" }));
        socket.send(JSON.stringify({ type: "close" }));
      }
      socket.close();
    }
    setLiveStatus("idle");
    setLiveUserDraft("");
    setAssistantDraft("");
  }

  function switchToTextMode() {
    if (inputMode === "text" && liveStatus === "idle") return;
    setInputMode("text");
    stopLiveMode();
    setStatus({ tone: "idle", text: "Text input ready" });
  }

  function switchToLiveMode() {
    setInputMode("live");
    if (liveStatus === "idle") {
      void startLiveMode();
    }
  }

  function toggleLiveMute() {
    if (inputMode !== "live" || liveStatus === "idle") return;
    setLiveMuted((current) => !current);
  }

  async function toggleLiveCapture() {
    if (inputMode !== "live" || liveStatus === "connecting") return;
    const socket = liveSocketRef.current;
    if (liveStatus === "listening") {
      stopLiveCapture();
      setLiveStatus("paused");
      setStatus({ tone: "idle", text: "Microphone paused" });
      return;
    }
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      if (liveStatus === "idle") {
        await startLiveMode();
      }
      return;
    }
    try {
      await startLiveCapture(socket);
    } catch (error) {
      setRuntimeError(errorMessage(error));
      setLiveStatus("error");
      setStatus({ tone: "error", text: "Microphone unavailable" });
    }
  }

  async function handleLiveRuntimeEvent(event: RuntimeEvent, socket: WebSocket) {
    if (event.type === "live_ready") {
      setStatus({ tone: "busy", text: "Live voice connected" });
      return;
    }
    if (event.type === "live_setup_complete") {
      try {
        await startLiveCapture(socket);
      } catch (error) {
        setRuntimeError(errorMessage(error));
        setLiveStatus("error");
        setStatus({ tone: "error", text: "Microphone unavailable" });
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "close" }));
        }
        socket.close();
      }
      return;
    }
    if (event.type === "live_transcript" && event.role === "user") {
      setLiveUserDraft((current) => current + (event.content || ""));
      return;
    }
    if (event.type === "live_transcript" && event.role === "assistant") {
      setAssistantDraft((current) => current + (event.content || ""));
      return;
    }
    if (event.type === "live_audio") {
      liveAudioChunkCountRef.current += 1;
      if (liveMutedRef.current) {
        setStatus({ tone: "busy", text: "Voice muted" });
      } else {
        setStatus({ tone: "busy", text: "Playing voice" });
        await queueLiveAudio(event.data);
      }
      return;
    }
    if (event.type === "live_interrupted") {
      setAssistantDraft("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      stopLivePlayback();
      setStatus({ tone: "idle", text: "Voice interrupted" });
      return;
    }
    if (event.type === "live_skill_start") {
      stopLivePlayback();
      setAssistantDraft("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      setStatus({ tone: "busy", text: "Running skill" });
      return;
    }
    if (event.type === "live_skill_result") {
      setStatus({ tone: "ok", text: "Skill completed" });
      return;
    }
    if (event.type === "message" && event.role === "assistant" && isLiveSkillEvent(event)) {
      handleRuntimeEvent(event);
      void refreshSessionData(sessionId, { revealNewArtifacts: true });
      return;
    }
    handleRuntimeEvent(event);
    if (event.type === "message" && event.role === "user") {
      setLiveUserDraft("");
    }
    if (event.type === "message" && event.role === "assistant") {
      setStatus(liveMutedRef.current
        ? { tone: "ok", text: "Voice response muted" }
        : liveAudioChunkCountRef.current > 0
          ? { tone: "ok", text: "Voice response played" }
          : { tone: "ok", text: "Voice transcript received" });
    }
  }

  async function startLiveCapture(socket: WebSocket) {
    if (socket.readyState !== WebSocket.OPEN) throw new Error("Live voice connection is not ready.");
    if (liveMediaRef.current) return;
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true }
    });
    const AudioContextCtor = window.AudioContext || (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioContextCtor) throw new Error("AudioContext is unavailable.");
    const audioContext = new AudioContextCtor();
    const source = audioContext.createMediaStreamSource(stream);
    const processor = audioContext.createScriptProcessor(4096, 1, 1);
    processor.onaudioprocess = (event) => {
      if (socket.readyState !== WebSocket.OPEN) return;
      const pcm = downsampleToPCM16(event.inputBuffer.getChannelData(0), audioContext.sampleRate, 16000);
      if (!pcm.length) return;
      socket.send(JSON.stringify({
        type: "audio",
        mime_type: "audio/pcm;rate=16000",
        data: bytesToBase64(pcm)
      }));
    };
    source.connect(processor);
    processor.connect(audioContext.destination);
    liveMediaRef.current = stream;
    liveAudioContextRef.current = audioContext;
    liveSourceRef.current = source;
    liveProcessorRef.current = processor;
    setLiveStatus("listening");
    setStatus({ tone: "busy", text: "Listening" });
  }

  function stopLiveCapture() {
    liveProcessorRef.current?.disconnect();
    liveSourceRef.current?.disconnect();
    liveMediaRef.current?.getTracks().forEach((track) => track.stop());
    void liveAudioContextRef.current?.close();
    liveProcessorRef.current = null;
    liveSourceRef.current = null;
    liveMediaRef.current = null;
    liveAudioContextRef.current = null;
  }

  function cleanupLiveAudio() {
    stopLiveCapture();
    stopLivePlayback();
    void livePlaybackContextRef.current?.close();
    livePlaybackContextRef.current = null;
  }

  function stopLivePlayback() {
    livePlaybackGenerationRef.current += 1;
    livePlaybackSourcesRef.current.forEach((source) => {
      try {
        source.stop();
      } catch {
        // Source may already have ended.
      }
    });
    livePlaybackSourcesRef.current.clear();
    livePlaybackTimeRef.current = 0;
    livePlaybackQueueRef.current = Promise.resolve();
  }

  async function ensureLivePlaybackContext(): Promise<AudioContext> {
    const AudioContextCtor = window.AudioContext || (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioContextCtor) throw new Error("AudioContext is unavailable.");
    const context = livePlaybackContextRef.current || new AudioContextCtor();
    livePlaybackContextRef.current = context;
    if (context.state === "suspended") {
      await context.resume();
    }
    return context;
  }

  async function queueLiveAudio(data: unknown) {
    const generation = livePlaybackGenerationRef.current;
    const next = livePlaybackQueueRef.current.catch(() => {}).then(() => {
      if (generation !== livePlaybackGenerationRef.current) return;
      return playLiveAudio(data, generation);
    });
    livePlaybackQueueRef.current = next;
    await next;
  }

  async function playLiveAudio(data: unknown, generation: number) {
    if (generation !== livePlaybackGenerationRef.current) return;
    const payload = data as { data?: string; mime_type?: string };
    if (!payload?.data) return;
    const sampleRate = sampleRateFromMime(payload.mime_type || "") || 24000;
    const samples = base64PCMToFloat32(payload.data, payload.mime_type || "");
    if (!samples.length) return;
    const context = await ensureLivePlaybackContext();
    if (generation !== livePlaybackGenerationRef.current) return;
    const buffer = context.createBuffer(1, samples.length, sampleRate);
    const channel = buffer.getChannelData(0);
    channel.set(samples);
    const source = context.createBufferSource();
    source.buffer = buffer;
    source.connect(context.destination);
    livePlaybackSourcesRef.current.add(source);
    source.onended = () => {
      livePlaybackSourcesRef.current.delete(source);
    };
    const startAt = Math.max(context.currentTime + 0.02, livePlaybackTimeRef.current || 0);
    source.start(startAt);
    livePlaybackTimeRef.current = startAt + buffer.duration;
  }

  async function uploadAttachment(fileList: FileList | null) {
    const file = fileList?.[0];
    if (!file) return;
    setUploading(true);
    setUploadError("");
    setUploadProgress(0);
    setStatus({ tone: "busy", text: "Uploading" });
    revealRightPanel("attachments");
    try {
      const uploaded = await api.uploadAttachment(file, sessionId || undefined, setUploadProgress);
      setPendingAttachments((current) => [...current, uploaded]);
      setAttachments(await api.attachments(sessionId || undefined));
      setStatus({ tone: "ok", text: "Uploaded" });
    } catch (error) {
      setUploadError(errorMessage(error));
      showError(error);
    } finally {
      setUploading(false);
      window.setTimeout(() => setUploadProgress(0), 700);
    }
  }

  function addAttachmentToMessage(asset: Asset) {
    setPendingAttachments((current) => {
      if (current.some((item) => item.id === asset.id)) return current;
      return [...current, asset];
    });
    setStatus({ tone: "ok", text: "Attachment added" });
  }

  function insertSkill(skill: Skill) {
    const nextDraft = `/${skill.name} `;
    setDraft(nextDraft);
    setSkillDetail(null);
    setRightPanelTab("skills");
    setRecentSkillNames((current) => {
      const next = [skill.name, ...current.filter((name) => name !== skill.name)].slice(0, 6);
      writeRecentSkills(next);
      return next;
    });
    setStatus({ tone: "ok", text: `Applied /${skill.name}` });
  }

  async function deleteAsset(kind: "attachment" | "artifact", id: string) {
    const confirmed = await requestConfirmation({
      title: `Delete ${kind}?`,
      message: `This will permanently delete this ${kind}.`,
      detail: kind === "attachment" ? "The uploaded file will be removed from this workspace." : "The generated artifact will be removed from this workspace.",
      confirmLabel: "Delete",
      danger: true
    });
    if (!confirmed) return;
    try {
      if (kind === "attachment") {
        await api.deleteAttachment(id);
        setAttachments(await api.attachments(sessionId || undefined));
      } else {
        await api.deleteArtifact(id);
        setArtifacts(await api.artifacts(sessionId || undefined));
      }
    } catch (error) {
      showError(error);
    }
  }

  async function extractAssetMemory(asset: Asset) {
    if (!memorySettings.capture_enabled) {
      setStatus({ tone: "error", text: "Memory saving is disabled" });
      return;
    }
    setAssetMemoryBusy((current) => ({ ...current, [asset.id]: true }));
    setStatus({ tone: "busy", text: "Extracting memory" });
    try {
      const items = await api.extractAssetMemory(asset, {
        namespace: "default",
        visibility: "user"
      });
      if (memoryManagerOpen) {
        await loadMemoryItems(memoryScope, memoryStatusFilter, memoryLevelFilter);
        await loadMemoryMaintenance();
      }
      setStatus({ tone: "ok", text: items.length ? `Saved ${items.length} memory item${items.length === 1 ? "" : "s"}` : "No durable memory found" });
    } catch (error) {
      showError(error);
    } finally {
      setAssetMemoryBusy((current) => {
        const next = { ...current };
        delete next[asset.id];
        return next;
      });
    }
  }

  function requestConfirmation(dialog: ConfirmDialog): Promise<boolean> {
    setConfirmDialog({
      cancelLabel: "Cancel",
      confirmLabel: "OK",
      ...dialog
    });
    return new Promise((resolve) => {
      confirmResolverRef.current = resolve;
    });
  }

  function resolveConfirmation(confirmed: boolean) {
    confirmResolverRef.current?.(confirmed);
    confirmResolverRef.current = null;
    setConfirmDialog(null);
  }

  async function cancelJob() {
    if (!selectedJobId) return;
    try {
      await api.cancelJob(selectedJobId);
      setJobs(await api.jobs(sessionId || undefined));
    } catch (error) {
      showError(error);
    }
  }

  function toggleJob(jobId: string) {
    setSelectedJobId((current) => current === jobId ? "" : jobId);
  }

  function handleRuntimeEvent(event: RuntimeEvent) {
    if (event.type === "message" && event.role === "user") {
      setLiveUserDraft("");
      setMessages((current) => appendRuntimeMessage(current, { role: "user", content: event.content || "" }));
    }
    if (event.type === "delta") {
      setAssistantDraft((current) => current + (event.content || ""));
    }
    if (event.type === "message" && event.role === "assistant") {
      setAssistantDraft("");
      setMessages((current) => appendRuntimeMessage(current, { role: "assistant", content: event.content || "" }));
    }
    if (event.type === "job" && event.job_id) {
      setSelectedJobId(event.job_id);
      revealRightPanel("jobs");
      setStatus({ tone: "busy", text: "Job started" });
      saveActiveJob(event.job_id, event.job?.session_id || event.session_id || sessionId);
      const submitted = event.job?.content || "";
      if (submitted && shouldDisplayJobSubmittedContent(event)) {
        const now = new Date().toISOString();
        setMessages((current) => appendRuntimeMessage(
          appendRuntimeMessage(current, { role: "user", content: submitted, created_at: now }),
          { role: "assistant", content: jobStartedMessage(event), created_at: now }
        ));
      }
      api.jobs(sessionId || undefined).then(setJobs).catch(() => {});
    }
    if (event.type === "error") {
      const message = event.error || "Request failed";
      setRuntimeError(message);
      setStatus({ tone: "error", text: message });
    }
    if (event.type === "done") {
      setStatus({ tone: "ok", text: "Done" });
    }
  }

  async function openJobStream(jobId: string, reconnect = false) {
    closeJobStream();
    activeJobStreamIdRef.current = jobId;
    jobStreamClosedRef.current = false;
    setJobStreamStatus(reconnect ? "reconnecting" : "connecting");
    setJobStreamNotice(reconnect ? "Reconnecting job stream..." : "Connecting job stream...");
    try {
      const events = await api.jobEvents(jobId);
      if (activeJobStreamIdRef.current !== jobId || jobStreamClosedRef.current) return;
      setJobEvents(events);
      lastJobEventRef.current = events[events.length - 1]?.id || "";
      const terminal = events.find((event) => terminalRuntimeEvents.has(event.type));
      if (terminal) {
        finishJobStream(jobId, terminal.event);
        return;
      }
    } catch (error) {
      if (activeJobStreamIdRef.current !== jobId || jobStreamClosedRef.current) return;
      setJobStreamStatus("failed");
      setJobStreamNotice(errorMessage(error));
      scheduleJobReconnect(jobId);
      return;
    }
    if (typeof EventSource === "undefined") {
      setJobStreamStatus("failed");
      setJobStreamNotice("Live job updates are unavailable in this browser.");
      return;
    }
    const source = new EventSource(api.jobStreamURL(jobId, lastJobEventRef.current), { withCredentials: true });
    jobSourceRef.current = source;
    source.onopen = () => {
      if (activeJobStreamIdRef.current !== jobId) return;
      jobReconnectAttemptRef.current = 0;
      setJobStreamStatus("live");
      setJobStreamNotice("");
    };
    const handle = (message: MessageEvent) => {
      try {
        const event = JSON.parse(message.data) as RuntimeEvent;
        const id = message.lastEventId || "";
        if (id) lastJobEventRef.current = id;
        setJobEvents((current) => appendJobEvent(current, { id: id || `${Date.now()}`, job_id: jobId, type: event.type, event, created_at: new Date().toISOString() }));
        if (terminalRuntimeEvents.has(event.type)) {
          finishJobStream(jobId, event);
        }
      } catch {
        // Ignore malformed stream frames.
      }
    };
    ["start", "message", "delta", "done", "error", "cancelled"].forEach((type) => source.addEventListener(type, handle));
    source.onerror = () => {
      source.close();
      jobSourceRef.current = null;
      if (jobStreamClosedRef.current || activeJobStreamIdRef.current !== jobId) return;
      setJobStreamStatus("reconnecting");
      setJobStreamNotice("Job stream disconnected. Reconnecting...");
      scheduleJobReconnect(jobId);
    };
  }

  function finishJobStream(jobId: string, event: RuntimeEvent) {
    jobStreamClosedRef.current = true;
    jobSourceRef.current?.close();
    jobSourceRef.current = null;
    clearJobReconnectTimer();
    setJobStreamStatus("idle");
    setJobStreamNotice("");
    clearActiveJob(jobId);
    api.jobs(sessionId || undefined).then(setJobs).catch(() => {});
    const targetSession = event.session_id || sessionId;
    if (targetSession) refreshSessionData(targetSession, { revealNewArtifacts: true }).catch(() => {});
  }

  function scheduleJobReconnect(jobId: string) {
    clearJobReconnectTimer();
    if (terminalJobs.has(jobs.find((job) => job.id === jobId)?.status || "")) {
      clearActiveJob(jobId);
      setJobStreamStatus("idle");
      setJobStreamNotice("");
      return;
    }
    const delay = Math.min(jobReconnectMaxMs, jobReconnectBaseMs * 2 ** jobReconnectAttemptRef.current);
    jobReconnectAttemptRef.current += 1;
    jobReconnectTimerRef.current = window.setTimeout(() => {
      jobReconnectTimerRef.current = null;
      if (jobStreamClosedRef.current || activeJobStreamIdRef.current !== jobId) return;
      openJobStream(jobId, true);
    }, delay);
  }

  function clearJobReconnectTimer() {
    if (jobReconnectTimerRef.current === null) return;
    window.clearTimeout(jobReconnectTimerRef.current);
    jobReconnectTimerRef.current = null;
  }

  function closeJobStream() {
    jobStreamClosedRef.current = true;
    clearJobReconnectTimer();
    jobSourceRef.current?.close();
    jobSourceRef.current = null;
  }

  function reconnectSelectedJob() {
    if (!selectedJobId) return;
    openJobStream(selectedJobId, true);
  }

  function showError(error: unknown) {
    const message = errorMessage(error);
    setRuntimeError(message);
    setStatus({ tone: "error", text: message });
    readServiceStatus(api).then(setServiceStatus).catch(() => {});
  }

  function updateRightSearch(value: string) {
    setRightPanelSearch((current) => ({ ...current, [rightPanelTab]: value }));
  }

  function leaveAdminConsole() {
    window.history.pushState({}, "", "/");
    setAdminView(false);
  }

  if (!authSession) {
    return (
      <main className="auth-page">
        <form className="auth-card" onSubmit={submitAuth}>
          <div className="auth-head">
            <BrandLogo />
            <div>
              <h1>AgentAPI</h1>
              <p>Consumer agent workspace</p>
            </div>
          </div>
          <div className="segmented">
            <button type="button" className={authMode === "login" ? "active" : ""} onClick={() => setAuthMode("login")}>Login</button>
            <button type="button" className={authMode === "register" ? "active" : ""} onClick={() => setAuthMode("register")}>Register</button>
          </div>
          <label>
            Email
            <input name="email" type="email" autoComplete="email" required />
          </label>
          {authMode === "register" && (
            <label>
              Name
              <input name="displayName" autoComplete="name" />
            </label>
          )}
          <label>
            Password
            <input name="password" type="password" autoComplete={authMode === "login" ? "current-password" : "new-password"} required minLength={8} />
          </label>
          {authMode === "register" && (
            <label>
              Confirm Password
              <input name="confirmPassword" type="password" autoComplete="new-password" required minLength={8} />
            </label>
          )}
          <button className="primary wide" type="submit">{authMode === "login" ? "Login" : "Create Account"}</button>
          <StatusLine status={status} />
        </form>
      </main>
    );
  }

  if (adminView) {
    return (
      <AdminConsole
        api={api}
        adminToken={adminToken}
        userLabel={authSession.user.display_name || authSession.user.email}
        onAdminTokenChange={updateAdminToken}
        onExit={leaveAdminConsole}
        onLogout={logout}
      />
    );
  }

  return (
    <main className={`app-shell ${leftSidebarOpen ? "" : "left-collapsed"} ${rightPanelOpen ? "" : "right-collapsed"}`}>
      <aside className={`sidebar ${mobileNav ? "open" : ""}`}>
        <div className="sidebar-head">
          <button
            className="brand-toggle"
            onClick={() => {
              setGlobalSearchOpen(false);
              setLeftSidebarOpen((open) => !open);
            }}
            title={leftSidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            aria-label={leftSidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            aria-expanded={leftSidebarOpen}
          >
            <BrandLogo className="brand-icon" />
            <span className="brand-toggle-icon"><PanelLeft size={18} /></span>
          </button>
          <BrandLogo className="brand-mark sidebar-logo" />
          <strong>AgentAPI</strong>
          <ServiceStatusPill status={serviceStatus} />
          <button
            className="icon sidebar-collapse-button"
            onClick={() => {
              setGlobalSearchOpen(false);
              setLeftSidebarOpen(false);
            }}
            title="Collapse sidebar"
            aria-label="Collapse sidebar"
            aria-expanded={leftSidebarOpen}
          >
            <PanelLeft size={18} />
          </button>
          <button className="icon ghost mobile-only" onClick={() => setMobileNav(false)} title="Close navigation" aria-label="Close navigation"><X size={18} /></button>
        </div>
        <div className="toolbar">
          <button className="icon primary" onClick={createSession} title="New session" aria-label="New session"><MessageSquarePlus size={18} /></button>
          <button
            className="icon"
            onClick={() => {
              setGlobalSearchOpen(true);
            }}
            title="Search messages"
            aria-label="Search messages"
            aria-pressed={globalSearchOpen}
          >
            <Search size={18} />
          </button>
          <button className="icon" onClick={refreshAll} title="Refresh" aria-label="Refresh"><RefreshCw size={18} /></button>
        </div>
        <div className="list sessions">
          {sessions.map((session) => (
            <div key={session.id} className={`list-item session-item ${session.id === sessionId ? "active" : ""}`}>
              <button className="session-select" onClick={() => { setSessionId(session.id); setMobileNav(false); }}>
                <span>{sessionTitle(session)}</span>
                <small>{session.message_count ?? (session.messages || []).filter((message) => !message.hidden).length} messages</small>
              </button>
              <button className="session-delete" onClick={() => removeSession(session.id)} title="Delete session" aria-label="Delete session">
                <Trash2 size={16} />
              </button>
            </div>
          ))}
        </div>
        <div className="account" ref={accountRef}>
          <div className="account-identity">
            <strong>{authSession.user.display_name || authSession.user.email}</strong>
            <small>{authSession.user.email}</small>
          </div>
          <button className="icon" onClick={() => setSettingsOpen((open) => !open)} title="Settings" aria-label="Settings"><Settings size={18} /></button>
          {settingsOpen && (
            <div className="settings-menu">
              <button onClick={() => { setSettingsOpen(false); setSettingsModalOpen(true); }}><Settings size={16} /> Settings</button>
              <button onClick={() => openMemoryManager("all")}><Database size={16} /> Manage Memory</button>
              <button onClick={logout}><LogOut size={16} /> Log Out</button>
            </div>
          )}
        </div>
      </aside>

      {globalSearchOpen && (
        <div className="global-search-overlay" onMouseDown={() => setGlobalSearchOpen(false)}>
          <section
            className="global-search-modal"
            ref={globalSearchDialogRef}
            role="dialog"
            aria-modal="true"
            aria-label="Search across all sessions"
            tabIndex={-1}
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="global-search-input">
              <Search size={18} />
              <input
                value={globalSearchQuery}
                onChange={(event) => setGlobalSearchQuery(event.target.value)}
                placeholder="Search across all sessions"
                aria-label="Search across all sessions"
                autoFocus
              />
              <button className="icon ghost" onClick={() => setGlobalSearchOpen(false)} title="Close search" aria-label="Close search">
                <X size={20} />
              </button>
            </div>
            <div className="global-search-results">
              {!globalSearchQuery.trim() && <div className="global-search-empty">Type to search conversations</div>}
              {globalSearchQuery.trim() && globalSearchLoading && <div className="global-search-empty">Searching...</div>}
              {globalSearchQuery.trim() && !globalSearchLoading && globalSearchError && <div className="global-search-empty error-text">{globalSearchError}</div>}
              {globalSearchQuery.trim() && !globalSearchLoading && !globalSearchError && !globalSearchResults.length && <div className="global-search-empty">No results</div>}
              {globalSearchQuery.trim() && !globalSearchLoading && !globalSearchError && globalSearchResults.map((result) => (
                <button key={`${result.session_id}-${result.message_index}-${result.created_at}`} className="global-search-result" onClick={() => openSearchResult(result)}>
                  <MessageCircle size={19} />
                  <span>
                    <strong>{result.session_title || result.session_id}</strong>
                    <small>{result.snippet || result.content || ""}</small>
                  </span>
                  <time>{formatTime(result.created_at)}</time>
                </button>
              ))}
            </div>
          </section>
        </div>
      )}

      <section className="workspace">
        <header className="topbar">
          <button className="icon mobile-only" onClick={() => setMobileNav(true)} title="Open navigation" aria-label="Open navigation"><Menu size={20} /></button>
          <div>
            <h2>{activeSession ? sessionTitle(activeSession) : "New conversation"}</h2>
            <StatusLine status={status} />
          </div>
        </header>
        {recoveryBanner && (
          <div className={`recovery-banner ${recoveryBanner.tone}`} role="status">
            <AlertCircle size={16} />
            <span>{recoveryBanner.text}</span>
            {online && selectedJobId && (
              <button className="inline" onClick={reconnectSelectedJob}>Reconnect</button>
            )}
          </div>
        )}
        <div className="messages" ref={messagesRef}>
          {!messages.length && !liveUserDraft && !assistantDraft && <div className="empty-state">Start with a message or choose a skill from the right panel.</div>}
          {messages.map((message, index) => (
            <MessageBubble
              key={`${message.created_at || index}-${index}`}
              message={message}
              highlighted={message.message_index !== undefined && message.message_index === highlightedMessageIndex}
            />
          ))}
          {liveUserDraft && <MessageBubble message={{ role: "user", content: liveUserDraft }} streaming />}
          {assistantDraft && <MessageBubble message={{ role: "assistant", content: assistantDraft }} streaming />}
        </div>
        <footer className="composer">
          {runtimeError && (
            <div className="composer-error" role="alert">
              <AlertCircle size={16} />
              <span>{runtimeError}</span>
              <button className="icon ghost" onClick={() => setRuntimeError("")} title="Dismiss error" aria-label="Dismiss error"><X size={14} /></button>
            </div>
          )}
          {uploadError && (
            <div className="composer-error upload-error" role="alert">
              <span>{uploadError}</span>
              <button className="icon ghost" onClick={() => setUploadError("")} title="Dismiss upload error" aria-label="Dismiss upload error"><X size={14} /></button>
            </div>
          )}
          {pendingAttachments.length > 0 && (
            <div className="pending-attachments" aria-label="Pending attachments">
              {pendingAttachments.map((asset) => (
                <span className="pending-attachment" key={asset.id} title={asset.filename}>
                  <FileUp size={13} />
                  <span>{asset.filename}</span>
                  <button
                    className="icon ghost"
                    onClick={() => setPendingAttachments((current) => current.filter((item) => item.id !== asset.id))}
                    title={`Remove ${asset.filename}`}
                    aria-label={`Remove ${asset.filename}`}
                  >
                    <X size={12} />
                  </button>
                </span>
              ))}
            </div>
          )}
          <div className="composer-row">
            <button
              type="button"
              className="composer-upload"
              title="Upload attachment"
              aria-label="Upload attachment"
              onClick={() => attachmentInputRef.current?.click()}
              disabled={uploading || inputMode !== "text" || liveStatus !== "idle"}
            >
              <FileUp size={18} />
            </button>
            <input
              ref={attachmentInputRef}
              className="composer-file-input"
              type="file"
              tabIndex={-1}
              aria-hidden="true"
              accept=".png,.jpg,.jpeg,.jfif,.webp,.gif,.avif,.bmp,.tif,.tiff,.heic,.heif,.pdf,.txt,.md,.csv,.json,.docx,.xlsx,.pptx,image/png,image/jpeg,image/pjpeg,image/webp,image/gif,image/avif,image/bmp,image/tiff,image/heic,image/heif,application/pdf,text/plain,text/markdown,text/csv,application/json"
              onChange={(event) => uploadAttachment(event.currentTarget.files)}
              disabled={uploading || inputMode !== "text" || liveStatus !== "idle"}
            />
            <textarea
              ref={composerInputRef}
              value={draft}
              aria-label="Message"
              placeholder={inputMode === "live" ? "Live mode is active" : "输入消息，或用 /skills 调用工作流"}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  sendMessage();
                }
              }}
              disabled={inputMode !== "text" || liveStatus !== "idle"}
              rows={1}
            />
            <div className="composer-actions">
              <div className="input-mode-toggle" role="group" aria-label="Input mode">
                <button
                  type="button"
                  className={inputMode === "text" ? "active" : ""}
                  onClick={switchToTextMode}
                  disabled={busyChat}
                  title="Text mode"
                  aria-label="Text mode"
                >
                  <MessageCircle size={16} />
                  <span>Text</span>
                </button>
                <button
                  type="button"
                  className={inputMode === "live" ? "active" : ""}
                  onClick={switchToLiveMode}
                  disabled={!sessionId || busyChat}
                  title="Live mode"
                  aria-label="Live mode"
                >
                  <Mic size={16} />
                  <span>Live</span>
                </button>
              </div>
              {inputMode === "live" && (
                <>
                  <button
                    type="button"
                    className={`voice-output-toggle ${liveMuted ? "muted" : ""}`}
                    onClick={toggleLiveMute}
                    disabled={liveStatus === "idle" || liveStatus === "connecting" || !sessionId}
                    title={liveMuted ? "Unmute voice output" : "Mute voice output"}
                    aria-label={liveMuted ? "Unmute voice output" : "Mute voice output"}
                    aria-pressed={liveStatus !== "idle" && !liveMuted}
                  >
                    {liveMuted ? <VolumeX size={18} /> : <Volume2 size={18} />}
                  </button>
                  <button
                    type="button"
                    className={`live-control ${liveStatus === "listening" ? "active" : ""}`}
                    onClick={() => void toggleLiveCapture()}
                    disabled={!sessionId || busyChat || liveStatus === "connecting" || liveStatus === "error"}
                    title={liveStatus === "listening" ? "Pause microphone" : "Resume microphone"}
                    aria-label={liveStatus === "listening" ? "Pause microphone" : "Resume microphone"}
                    aria-pressed={liveStatus === "listening"}
                  >
                    {liveStatus === "listening" ? <Mic size={18} /> : <MicOff size={18} />}
                  </button>
                </>
              )}
              {busyChat ? (
                <button className="stop-generation" onClick={cancelChat} title="Stop generation" aria-label="Stop generation">
                  <span><Square size={16} fill="currentColor" /></span>
                </button>
              ) : (
                <button className="primary send" onClick={sendMessage} disabled={inputMode !== "text" || liveStatus !== "idle" || (!draft.trim() && pendingAttachments.length === 0) || !sessionId} title="Send" aria-label="Send">
                  <Send size={21} />
                </button>
              )}
            </div>
          </div>
        </footer>
      </section>

      <button
        className="right-panel-toggle"
        onClick={() => setRightPanelOpen((open) => !open)}
        aria-label={rightPanelOpen ? "Collapse right panel" : "Expand right panel"}
        title={rightPanelOpen ? "Collapse right panel" : "Expand right panel"}
        aria-expanded={rightPanelOpen}
      >
        <PanelLeft size={18} />
      </button>

      <aside className="right-panel" aria-hidden={!rightPanelOpen}>
        <div className="right-tabs" role="tablist" aria-label="Right panel tools">
          <RightTabButton tab="skills" activeTab={rightPanelTab} label="Skills" count={skills.length} icon={<Sparkles size={20} />} onClick={setRightPanelTab} />
          <RightTabButton tab="jobs" activeTab={rightPanelTab} label="Jobs" count={jobs.length} icon={<Briefcase size={20} />} onClick={setRightPanelTab} />
          <RightTabButton tab="attachments" activeTab={rightPanelTab} label="Attachments" count={attachments.length} icon={<FileUp size={20} />} onClick={setRightPanelTab} />
          <RightTabButton tab="artifacts" activeTab={rightPanelTab} label="Artifacts" count={artifacts.length} icon={<Image size={20} />} onClick={setRightPanelTab} />
        </div>
        <div className="right-search">
          <Search size={16} />
          <input
            value={rightSearch}
            onChange={(event) => updateRightSearch(event.target.value)}
            placeholder={`Search ${rightPanelLabel(rightPanelTab)}`}
            aria-label={`Search ${rightPanelLabel(rightPanelTab)}`}
          />
          {rightSearch && (
            <button className="icon ghost" onClick={() => updateRightSearch("")} aria-label="Clear search" title="Clear search">
              <X size={14} />
            </button>
          )}
        </div>
        <div className="right-tab-content">
          {rightPanelTab === "skills" && (
            <SkillPanel
              skills={filteredSkills}
              recentSkillNames={recentSkillNames}
              emptyLabel={rightPanelSearch.skills ? "No matching skills" : "No skills"}
              onInsert={insertSkill}
              onDetails={setSkillDetail}
            />
          )}
          {rightPanelTab === "jobs" && (
            <>
              {filteredJobs.length ? (
                <div className="list compact job-list">
                  {filteredJobs.map((job) => {
                    const expanded = job.id === selectedJobId;
                    return (
                      <section key={job.id} className={`job-list-entry ${expanded ? "expanded" : ""}`}>
                        <button
                          className={`list-item job-summary ${expanded ? "active" : ""}`}
                          onClick={() => toggleJob(job.id)}
                          aria-expanded={expanded}
                        >
                          <span>{job.content || job.id}</span>
                          <small>{job.status} · {formatTime(job.updated_at)}</small>
                          <ChevronDown size={16} aria-hidden="true" />
                        </button>
                        {expanded && (
                          <div className="job-expanded">
                            <div className="job-card">
                              <div className={`pill ${job.status}`}>{job.status}</div>
                              {jobStreamNotice && !terminalJobs.has(job.status) && (
                                <span className={`job-stream-state ${jobStreamStatus}`}>{jobStreamNotice}</span>
                              )}
                              <button className="danger inline" disabled={terminalJobs.has(job.status)} onClick={cancelJob}>Cancel job</button>
                            </div>
                            <div className="timeline">
                              {visibleJobEvents(jobEvents).map((event) => (
                                <div key={event.id} className="timeline-row">
                                  <span>{event.type}</span>
                                  <p>{event.event.error || event.event.content || event.event.job_reason || event.id}</p>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </section>
                    );
                  })}
                </div>
              ) : (
                <div className="empty-small">{rightPanelSearch.jobs ? "No results" : "No items"}</div>
              )}
            </>
          )}
          {rightPanelTab === "attachments" && (
            <>
              {uploadProgress > 0 && <Progress value={uploadProgress} />}
              <AssetList
                assets={filteredAttachments}
                icon="file"
                emptyLabel={rightPanelSearch.attachments ? "No results" : "No items"}
                preview={(asset) => setPreviewAsset({ asset, url: api.attachmentURL(asset.id) })}
                download={(id) => window.open(api.attachmentURL(id), "_blank")}
                remove={(id) => deleteAsset("attachment", id)}
                extractMemory={extractAssetMemory}
                memoryBusy={assetMemoryBusy}
                memoryDisabled={!memorySettings.capture_enabled}
                addToMessage={addAttachmentToMessage}
              />
            </>
          )}
          {rightPanelTab === "artifacts" && (
            <AssetList
              assets={filteredArtifacts}
              icon="image"
              emptyLabel={rightPanelSearch.artifacts ? "No results" : "No items"}
              preview={(asset) => setPreviewAsset({ asset, url: api.artifactURL(asset.id) })}
              download={(id) => window.open(api.artifactURL(id), "_blank")}
              remove={(id) => deleteAsset("artifact", id)}
              extractMemory={extractAssetMemory}
              memoryBusy={assetMemoryBusy}
              memoryDisabled={!memorySettings.capture_enabled}
            />
          )}
        </div>
      </aside>
      {skillDetail && (
        <SkillDetailModal
          refElement={skillDetailDialogRef}
          skill={skillDetail}
          onInsert={insertSkill}
          onClose={() => setSkillDetail(null)}
        />
      )}
      {memoryManagerOpen && (
        <MemoryModal
          items={memoryItems}
          actions={memoryActions}
          loading={memoryLoading}
          error={memoryError}
          scope={memoryScope}
          statusFilter={memoryStatusFilter}
          levelFilter={memoryLevelFilter}
          hasSession={Boolean(sessionId)}
          onScopeChange={(scope) => {
            setMemoryScope(scope);
            void loadMemoryItems(scope, memoryStatusFilter, memoryLevelFilter);
          }}
          onStatusChange={(status) => {
            setMemoryStatusFilter(status);
            void loadMemoryItems(memoryScope, status, memoryLevelFilter);
          }}
          onLevelChange={(level) => {
            setMemoryLevelFilter(level);
            void loadMemoryItems(memoryScope, memoryStatusFilter, level);
          }}
          onRefresh={() => loadMemoryItems(memoryScope, memoryStatusFilter, memoryLevelFilter)}
          onRebuild={rebuildMemoryAbstractions}
          onScore={scoreMemoryQuality}
          onRunMaintenance={runMemoryMaintenance}
          onUpdate={updateMemoryItem}
          onFeedback={sendMemoryFeedback}
          onResolve={resolveMemoryConflict}
          onDelete={deleteMemoryItem}
          onClose={() => setMemoryManagerOpen(false)}
        />
      )}
      {settingsModalOpen && (
        <SettingsModal
          userLabel={authSession.user.display_name || authSession.user.email}
          memorySettings={memorySettings}
          personalizationSettings={personalizationSettings}
          personalizationSaving={personalizationSaving}
          hasSession={Boolean(sessionId)}
          onUpdateMemorySettings={updateMemorySettings}
          onUpdatePersonalization={updatePersonalizationSettings}
          onResetPersonalization={resetPersonalizationSettings}
          onManageMemory={() => openMemoryManager("all")}
          onDeleteSessionMemory={deleteSessionMemory}
          onDeleteAllMemory={deleteAllMemory}
          onExportData={exportData}
          onDeleteAccount={deleteAccount}
          onLogout={logout}
          onClose={() => setSettingsModalOpen(false)}
        />
      )}
      {previewAsset && <PreviewModal asset={previewAsset.asset} url={previewAsset.url} onClose={() => setPreviewAsset(null)} />}
      {confirmDialog && (
        <ConfirmModal
          dialog={confirmDialog}
          onCancel={() => resolveConfirmation(false)}
          onConfirm={() => resolveConfirmation(true)}
        />
      )}
    </main>
  );
}

async function bootstrap(
  api: ApiClient,
  setStatus: (status: Status) => void,
  setSessions: (sessions: Session[]) => void,
  setSessionId: (sessionId: string) => void,
  setMessages: (messages: Message[]) => void,
  setSkills: (skills: Skill[]) => void,
  setJobs: (jobs: Job[]) => void,
  setAttachments: (assets: Asset[]) => void,
  setArtifacts: (assets: Asset[]) => void
) {
  setStatus({ tone: "busy", text: "Loading" });
  let sessionList = await api.sessions();
  if (!sessionList.length) {
    const session = await api.createSession();
    sessionList = [session];
  }
  const storedJob = loadActiveJob();
  const current = sessionList.find((session) => session.id === storedJob?.sessionId) || sessionList[0];
  const [skills, jobs, attachments, artifacts] = await Promise.all([
    api.skills(),
    api.jobs(current.id),
    api.attachments(current.id),
    api.artifacts(current.id)
  ]);
  setSessions(sessionList);
  setSessionId(current.id);
  setMessages(visibleMessages(current.messages || []));
  setSkills(skills);
  setJobs(jobs);
  setAttachments(attachments);
  setArtifacts(artifacts);
  setStatus({ tone: "ok", text: "Ready" });
}

function visibleMessages(messages: Message[]): Message[] {
  const visible: Message[] = [];
  messages.forEach((message, index) => {
    const indexed = { ...message, message_index: message.message_index ?? index };
    const syntheticSkillUser = generatedSkillUserMessage(indexed);
    if (syntheticSkillUser) {
      const previous = visible[visible.length - 1];
      if (previous?.role !== "user" || previous.content !== syntheticSkillUser.content) {
        visible.push(syntheticSkillUser);
      }
      return;
    }
    if (isConvertedSkillCommandMessage(visible, indexed)) {
      return;
    }
    if (indexed.role !== "tool" && !indexed.hidden && (indexed.content || indexed.tool_output)) {
      visible.push(indexed);
    }
  });
  return visible;
}

function generatedSkillUserMessage(message: Message): Message | null {
  if (!message.hidden || message.role !== "user" || !message.content?.includes("<skill-format>true</skill-format>")) return null;
  const name = tagContent(message.content, "command-name") || tagContent(message.content, "command-message");
  if (!name) return null;
  const args = tagContent(message.content, "command-args");
  return {
    role: "user",
    content: [name.startsWith("/") ? name : `/${name}`, args].filter(Boolean).join(" "),
    created_at: message.created_at,
    message_index: message.message_index
  };
}

function tagContent(content: string, tag: string): string {
  const match = content.match(new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`));
  return match ? match[1].trim() : "";
}

function upsertSession(items: Session[], session: Session): Session[] {
  const next = items.filter((item) => item.id !== session.id);
  return [session, ...next].sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime());
}

function appendRuntimeMessage(messages: Message[], message: Message): Message[] {
  if (isConvertedSkillCommandMessage(messages, message)) {
    return messages;
  }
  const content = message.content || message.tool_output || "";
  const previous = messages[messages.length - 1];
  const previousContent = previous?.content || previous?.tool_output || "";
  if (previous?.role === message.role && (previousContent === content || previousContent.startsWith(`${content}\n\nAttachments:`))) {
    return messages;
  }
  return [...messages, message];
}

function isLiveSkillEvent(event: RuntimeEvent): boolean {
  const data = event.data as { source?: unknown } | undefined;
  return data?.source === "live_skill";
}

function isConvertedSkillCommandMessage(messages: Message[], message: Message): boolean {
  const content = (message.content || message.tool_output || "").trim();
  if (message.hidden || message.role !== "user" || !isSlashSkillCommand(content)) {
    return false;
  }
  const previous = lastVisibleConversationalMessage(messages);
  const previousContent = (previous?.content || previous?.tool_output || "").trim();
  return previous?.role === "user" && !!previousContent && !isSlashSkillCommand(previousContent);
}

function lastVisibleConversationalMessage(messages: Message[]): Message | undefined {
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index];
    const content = (message.content || message.tool_output || "").trim();
    if (message.hidden || message.role === "tool" || !content) {
      continue;
    }
    if (message.role === "user" || message.role === "assistant") {
      return message;
    }
  }
  return undefined;
}

function isSlashSkillCommand(content: string): boolean {
  return /^\/[A-Za-z0-9_.:-]+(?:\s|$)/.test(content.trim());
}

function appendJobEvent(events: JobEvent[], event: JobEvent): JobEvent[] {
  if (events.some((item) => item.id === event.id)) return events;
  return [...events, event];
}

function loadActiveJob(): { jobId: string; sessionId: string } | null {
  try {
    const raw = localStorage.getItem(activeJobStorageKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { jobId?: string; sessionId?: string };
    if (!parsed.jobId || !parsed.sessionId) return null;
    return { jobId: parsed.jobId, sessionId: parsed.sessionId };
  } catch {
    return null;
  }
}

function saveActiveJob(jobId: string, sessionId: string) {
  if (!jobId || !sessionId) return;
  try {
    localStorage.setItem(activeJobStorageKey, JSON.stringify({ jobId, sessionId }));
  } catch {
    // Best-effort recovery hint only.
  }
}

function clearActiveJob(jobId?: string) {
  try {
    const stored = loadActiveJob();
    if (jobId && stored?.jobId && stored.jobId !== jobId) return;
    localStorage.removeItem(activeJobStorageKey);
  } catch {
    // Best-effort recovery hint only.
  }
}

function messageWithAttachmentNames(content: string, assets: Asset[]): string {
  if (assets.length === 0) return content;
  const names = assets.map((asset) => asset.filename).join(", ");
  return `${content}\n\nAttachments: ${names}`;
}

function MessageBubble({ message, streaming = false, highlighted = false }: { message: Message; streaming?: boolean; highlighted?: boolean }) {
  const text = message.content || message.tool_output || "";
  const rendered = splitAttachedTextSections(text);
  const isAssistant = message.role !== "user";
  return (
    <article
      className={`message ${message.role === "user" ? "user" : "assistant"} ${highlighted ? "highlighted" : ""}`}
      data-message-index={message.message_index}
    >
      <div className="message-role">{message.role === "user" ? "You" : "Agent"}{streaming ? " ..." : ""}</div>
      {isAssistant ? <MarkdownContent text={rendered.text} /> : <div className="message-text">{rendered.text}</div>}
      {rendered.attachments.length > 0 && (
        <div className="message-attachment-previews">
          {rendered.attachments.map((attachment, index) => (
            <MessageAttachmentPreview key={`${attachment.filename}-${index}`} attachment={attachment} />
          ))}
        </div>
      )}
    </article>
  );
}

type MarkdownBlock =
  | { type: "paragraph"; lines: string[] }
  | { type: "heading"; level: number; text: string }
  | { type: "code"; language: string; text: string }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "quote"; lines: string[] }
  | { type: "table"; headers: string[]; rows: string[][] };

function MarkdownContent({ text }: { text: string }) {
  const blocks = parseMarkdown(text);
  if (blocks.length === 0) return <div className="message-text" />;
  return (
    <div className="markdown-content">
      {blocks.map((block, index) => renderMarkdownBlock(block, index))}
    </div>
  );
}

function parseMarkdown(text: string): MarkdownBlock[] {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  let paragraph: string[] = [];

  const flushParagraph = () => {
    const content = paragraph.join("\n").trim();
    if (content) blocks.push({ type: "paragraph", lines: paragraph });
    paragraph = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      flushParagraph();
      continue;
    }

    const fence = trimmed.match(/^```([\w+-]*)\s*$/);
    if (fence) {
      flushParagraph();
      const code: string[] = [];
      i++;
      for (; i < lines.length; i++) {
        if (lines[i].trim() === "```") break;
        code.push(lines[i]);
      }
      blocks.push({ type: "code", language: fence[1] || "", text: code.join("\n") });
      continue;
    }

    const heading = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      flushParagraph();
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2].trim() });
      continue;
    }

    if (isTableStart(lines, i)) {
      flushParagraph();
      const headers = splitTableRow(lines[i]);
      const rows: string[][] = [];
      i += 2;
      for (; i < lines.length && lines[i].includes("|") && lines[i].trim(); i++) {
        rows.push(splitTableRow(lines[i]));
      }
      i--;
      blocks.push({ type: "table", headers, rows });
      continue;
    }

    const bullet = parseListItem(trimmed);
    if (bullet) {
      flushParagraph();
      const items = [bullet.text];
      const ordered = bullet.ordered;
      for (let j = i + 1; j < lines.length; j++) {
        const next = parseListItem(lines[j].trim());
        if (!next || next.ordered !== ordered) break;
        items.push(next.text);
        i = j;
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }

    if (trimmed.startsWith(">")) {
      flushParagraph();
      const quoteLines = [trimmed.replace(/^>\s?/, "")];
      for (let j = i + 1; j < lines.length; j++) {
        const next = lines[j].trim();
        if (!next.startsWith(">")) break;
        quoteLines.push(next.replace(/^>\s?/, ""));
        i = j;
      }
      blocks.push({ type: "quote", lines: quoteLines });
      continue;
    }

    paragraph.push(line);
  }

  flushParagraph();
  return blocks;
}

function renderMarkdownBlock(block: MarkdownBlock, index: number): ReactNode {
  switch (block.type) {
    case "heading": {
      const Heading = `h${Math.min(block.level + 1, 6)}` as keyof JSX.IntrinsicElements;
      return <Heading key={index}>{renderInlineMarkdown(block.text, `h-${index}`)}</Heading>;
    }
    case "code":
      return (
        <pre key={index} className="markdown-code">
          {block.language && <span>{block.language}</span>}
          <code>{block.text}</code>
        </pre>
      );
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List key={index}>
          {block.items.map((item, itemIndex) => (
            <li key={itemIndex}>{renderInlineMarkdown(item, `li-${index}-${itemIndex}`)}</li>
          ))}
        </List>
      );
    }
    case "quote":
      return <blockquote key={index}>{renderInlineMarkdown(block.lines.join("\n"), `q-${index}`)}</blockquote>;
    case "table":
      return (
        <div key={index} className="markdown-table-wrap">
          <table>
            <thead>
              <tr>{block.headers.map((cell, cellIndex) => <th key={cellIndex}>{renderInlineMarkdown(cell, `th-${index}-${cellIndex}`)}</th>)}</tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex}>{row.map((cell, cellIndex) => <td key={cellIndex}>{renderInlineMarkdown(cell, `td-${index}-${rowIndex}-${cellIndex}`)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "paragraph":
    default:
      return <p key={index}>{renderInlineMarkdown(block.lines.join("\n"), `p-${index}`)}</p>;
  }
}

function parseListItem(line: string): { ordered: boolean; text: string } | null {
  const ordered = line.match(/^\d+[.)]\s+(.+)$/);
  if (ordered) return { ordered: true, text: ordered[1] };
  const bullet = line.match(/^[-*+]\s+(.+)$/);
  if (bullet) return { ordered: false, text: bullet[1] };
  return null;
}

function isTableStart(lines: string[], index: number): boolean {
  if (!lines[index]?.includes("|") || !lines[index + 1]?.includes("|")) return false;
  return splitTableRow(lines[index + 1]).every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

function splitTableRow(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim());
}

function renderInlineMarkdown(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  let index = 0;
  let key = 0;

  const pushText = (value: string) => {
    if (!value) return;
    const pieces = value.split("\n");
    pieces.forEach((piece, pieceIndex) => {
      if (piece) nodes.push(piece);
      if (pieceIndex < pieces.length - 1) nodes.push(<br key={`${keyPrefix}-br-${key++}`} />);
    });
  };

  while (index < text.length) {
    const candidates = [
      { marker: "`", pos: text.indexOf("`", index) },
      { marker: "**", pos: text.indexOf("**", index) },
      { marker: "*", pos: text.indexOf("*", index) },
      { marker: "[", pos: text.indexOf("[", index) }
    ].filter((candidate) => candidate.pos >= 0).sort((a, b) => a.pos - b.pos || b.marker.length - a.marker.length);

    const next = candidates[0];
    if (!next) {
      pushText(text.slice(index));
      break;
    }
    pushText(text.slice(index, next.pos));

    if (next.marker === "`") {
      const end = text.indexOf("`", next.pos + 1);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<code key={`${keyPrefix}-code-${key++}`}>{text.slice(next.pos + 1, end)}</code>);
      index = end + 1;
      continue;
    }

    if (next.marker === "**") {
      const end = text.indexOf("**", next.pos + 2);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<strong key={`${keyPrefix}-strong-${key++}`}>{renderInlineMarkdown(text.slice(next.pos + 2, end), `${keyPrefix}-strong-${key}`)}</strong>);
      index = end + 2;
      continue;
    }

    if (next.marker === "*") {
      if (text[next.pos + 1] === "*") {
        index = next.pos + 1;
        continue;
      }
      const end = text.indexOf("*", next.pos + 1);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<em key={`${keyPrefix}-em-${key++}`}>{renderInlineMarkdown(text.slice(next.pos + 1, end), `${keyPrefix}-em-${key}`)}</em>);
      index = end + 1;
      continue;
    }

    const labelEnd = text.indexOf("]", next.pos + 1);
    const urlStart = labelEnd >= 0 ? text.indexOf("(", labelEnd + 1) : -1;
    const urlEnd = urlStart >= 0 ? text.indexOf(")", urlStart + 1) : -1;
    if (labelEnd < 0 || urlStart !== labelEnd + 1 || urlEnd < 0) {
      pushText(text[next.pos]);
      index = next.pos + 1;
      continue;
    }
    const label = text.slice(next.pos + 1, labelEnd);
    const href = text.slice(urlStart + 1, urlEnd).trim();
    if (isSafeHref(href)) {
      nodes.push(
        <a key={`${keyPrefix}-a-${key++}`} href={href} target="_blank" rel="noreferrer">
          {renderInlineMarkdown(label, `${keyPrefix}-a-${key}`)}
        </a>
      );
    } else {
      pushText(text.slice(next.pos, urlEnd + 1));
    }
    index = urlEnd + 1;
  }

  return nodes;
}

function isSafeHref(href: string): boolean {
  return /^(https?:|mailto:)/i.test(href);
}

type AttachedTextSection = {
  filename: string;
  contentType: string;
  content: string;
};

function MessageAttachmentPreview({ attachment }: { attachment: AttachedTextSection }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <section className={`message-attachment-preview ${expanded ? "expanded" : ""}`}>
      <button type="button" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
        <FileText size={15} />
        <span>
          <strong>{attachment.filename}</strong>
          <small>{attachment.contentType || "text"} · {expanded ? "Hide content" : "Show content"}</small>
        </span>
      </button>
      {expanded && (
        <pre>{attachment.content}</pre>
      )}
    </section>
  );
}

function splitAttachedTextSections(text: string): { text: string; attachments: AttachedTextSection[] } {
  const attachments: AttachedTextSection[] = [];
  const pattern = /(?:\n{2}|^)Attached text file: ([^\n]+)\nContent-Type: ([^\n]+)\n\n```text\n([\s\S]*?)\n```/g;
  const cleaned = text.replace(pattern, (_match, filename: string, contentType: string, content: string) => {
    attachments.push({
      filename: filename.trim(),
      contentType: contentType.trim(),
      content
    });
    return "";
  }).trim();
  return { text: cleaned, attachments };
}

function StatusLine({ status }: { status: Status }) {
  if (status.tone !== "busy" && status.tone !== "error") return null;
  return (
    <div className={`status ${status.tone}`}>
      <Activity size={14} />
      <span>{status.text}</span>
    </div>
  );
}

function errorMessage(error: unknown): string {
  return error instanceof ApiError && error.requestId
    ? `${error.message} (${error.requestId})`
    : error instanceof Error
      ? error.message
      : String(error);
}

function ServiceStatusPill({ status }: { status: ServiceStatus }) {
  return (
    <div className={`service-status ${status.tone}`} title={status.details || status.text}>
      <Activity size={13} />
      <span>{status.text}</span>
    </div>
  );
}

function jobStartedMessage(event: RuntimeEvent): string {
  if (event.job?.type === "skill") {
    return "已开始执行工作流，完成后会自动更新结果。你也可以在右侧 Jobs 面板查看进度。";
  }
  return "已开始后台处理，完成后会自动更新结果。你也可以在右侧 Jobs 面板查看进度。";
}

function shouldDisplayJobSubmittedContent(event: RuntimeEvent): boolean {
  if (event.job_reason === "skill metadata requests durable job execution") {
    return false;
  }
  return true;
}

function visibleJobEvents(events: JobEvent[]): JobEvent[] {
  return events.filter((event) => !(event.type === "delta" && event.event.role === "assistant"));
}

async function readServiceStatus(api: ApiClient): Promise<ServiceStatus> {
  try {
    return readinessToServiceStatus(await api.readiness());
  } catch (error) {
    return {
      tone: "error",
      text: "Offline",
      details: error instanceof Error ? error.message : String(error)
    };
  }
}

function readinessToServiceStatus(readiness: ReadinessStatus): ServiceStatus {
  const failed = (readiness.checks || []).filter((check) => check.status !== "ok");
  if (readiness.status === "ok" && failed.length === 0) {
    return { tone: "ok", text: "Available", details: "All readiness checks passed" };
  }
  const details = failed.length
    ? failed.map((check) => `${check.name}: ${check.error || check.status}`).join("\n")
    : `readiness: ${readiness.status}`;
  return { tone: "error", text: "Degraded", details };
}

function RightTabButton({
  tab,
  activeTab,
  label,
  count,
  icon,
  onClick
}: {
  tab: RightPanelTab;
  activeTab: RightPanelTab;
  label: string;
  count: number;
  icon: ReactNode;
  onClick: (tab: RightPanelTab) => void;
}) {
  const active = tab === activeTab;
  return (
    <button
      className={`right-tab ${active ? "active" : ""}`}
      onClick={() => onClick(tab)}
      role="tab"
      aria-selected={active}
      title={label}
      aria-label={label}
    >
      {icon}
      <span className="tab-count">{count}</span>
    </button>
  );
}

function SkillPanel({
  skills,
  recentSkillNames,
  emptyLabel,
  onInsert,
  onDetails
}: {
  skills: Skill[];
  recentSkillNames: string[];
  emptyLabel: string;
  onInsert: (skill: Skill) => void;
  onDetails: (skill: Skill) => void;
}) {
  const recentSkills = recentSkillsFromNames(skills, recentSkillNames);
  const orderedSkills = useMemo(() => [...skills].sort(compareSkills), [skills]);
  const [expandedSkillName, setExpandedSkillName] = useState("");
  const skillRefs = useRef(new Map<string, HTMLElement>());
  if (!orderedSkills.length) return <div className="empty-small">{emptyLabel}</div>;
  const jumpToSkill = (skill: Skill) => {
    setExpandedSkillName(skill.name);
    window.requestAnimationFrame(() => {
      skillRefs.current.get(skill.name)?.scrollIntoView({ block: "start", behavior: "smooth" });
    });
  };
  const setSkillRef = (name: string) => (element: HTMLElement | null) => {
    if (element) skillRefs.current.set(name, element);
    else skillRefs.current.delete(name);
  };
  return (
    <div className="skill-browser">
      {recentSkills.length > 0 && (
        <section className="skill-section">
          <div className="skill-section-title">
            <Star size={14} />
            <span>Recent</span>
            <small>{recentSkills.length}</small>
          </div>
          <div className="recent-skill-list">
            {recentSkills.map((skill) => (
              <button key={skill.name} type="button" onClick={() => jumpToSkill(skill)}>
                <SkillGlyph skill={skill} />
                <span>{skill.display_name || skill.name}</span>
              </button>
            ))}
          </div>
        </section>
      )}
      <section className="skill-section">
        <div className="skill-section-title">
          <Sparkles size={14} />
          <span>Skills</span>
          <small>{orderedSkills.length}</small>
        </div>
        <div className="skill-grid">
          {orderedSkills.map((skill) => {
            const expanded = expandedSkillName === skill.name;
            return (
              <SkillCard
                key={skill.name}
                ref={setSkillRef(skill.name)}
                skill={skill}
                expanded={expanded}
                onToggle={() => setExpandedSkillName(expanded ? "" : skill.name)}
                onInsert={onInsert}
                onDetails={onDetails}
              />
            );
          })}
        </div>
      </section>
    </div>
  );
}

const SkillCard = forwardRef<HTMLElement, {
  skill: Skill;
  expanded: boolean;
  onToggle: () => void;
  onInsert: (skill: Skill) => void;
  onDetails: (skill: Skill) => void;
}>(function SkillCard({
  skill,
  expanded,
  onToggle,
  onInsert,
  onDetails
}, ref) {
  const title = skill.display_name || skill.name;
  return (
    <article ref={ref} className={`skill-card ${expanded ? "expanded" : "collapsed"} ${skill.featured ? "featured" : ""}`}>
      <button type="button" className="skill-card-summary" onClick={onToggle} aria-expanded={expanded}>
        <SkillGlyph skill={skill} />
        <div>
          <strong>{title}</strong>
          <small>/{skill.name}</small>
        </div>
        <ChevronDown size={16} />
      </button>
      {expanded && (
        <>
          <p>{skill.short_description || skill.description || skill.usage || "No description available."}</p>
          <div className="skill-meta-row">
            {skill.run_as_job && <span>Job</span>}
            {skill.produces_artifacts && <span>Artifacts</span>}
            {skill.version && <span>v{skill.version}</span>}
          </div>
          <div className="skill-card-actions">
            <button type="button" className="skill-action primary" onClick={() => onInsert(skill)} title={`Apply /${skill.name}`} aria-label={`Apply /${skill.name}`}>
              <PlayCircle size={15} />
              <span>Apply</span>
            </button>
            <button type="button" className="skill-action" onClick={() => onDetails(skill)} title={`Details for ${title}`} aria-label="Skill details">
              <Info size={15} />
            </button>
          </div>
        </>
    )}
  </article>
);
});

function AdminTabs<T extends string>({
  tabs,
  active,
  onChange,
  label,
  compact = false
}: {
  tabs: Array<AdminTabOption<T>>;
  active: T;
  onChange: (tab: T) => void;
  label: string;
  compact?: boolean;
}) {
  return (
    <nav className={`admin-tabs${compact ? " compact" : ""}`} aria-label={label}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          className={tab.id === active ? "active" : ""}
          onClick={() => onChange(tab.id)}
        >
          {tab.icon}
          <span>{tab.label}</span>
          {typeof tab.count === "number" && <small>{tab.count}</small>}
        </button>
      ))}
    </nav>
  );
}

function AdminConsole({
  api,
  adminToken,
  userLabel,
  onAdminTokenChange,
  onExit,
  onLogout
}: {
  api: ApiClient;
  adminToken: string;
  userLabel: string;
  onAdminTokenChange: (token: string) => void;
  onExit: () => void;
  onLogout: () => void;
}) {
  const [adminSection, setAdminSection] = useState<AdminSection>("skills");
  const [skills, setSkills] = useState<AdminSkill[]>([]);
  const [selectedName, setSelectedName] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [loading, setLoading] = useState(false);
  const [detailsLoading, setDetailsLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [review, setReview] = useState<SkillReviewResult | null>(null);
  const [versions, setVersions] = useState<SkillVersion[]>([]);
  const [executions, setExecutions] = useState<SkillExecution[]>([]);
  const [summary, setSummary] = useState<SkillExecutionSummary | null>(null);
  const [policyTarget, setPolicyTarget] = useState<AdminSkill | null>(null);
  const [skillTab, setSkillTab] = useState<"overview" | "review" | "executions" | "versions">("overview");
  const token = adminToken.trim();
  const adminSections: Array<AdminTabOption<AdminSection>> = [
    { id: "skills", label: "Skills", description: "Publish, review, configure policy, and inspect execution health for registry-backed skills.", icon: <Sparkles size={18} />, count: skills.length },
    { id: "users", label: "Users", description: "Search users, inspect account state, and disable, ban, or reactivate access.", icon: <Database size={18} /> },
    { id: "jobs-assets", label: "Jobs & assets", description: "Inspect a user's sessions, queued jobs, replay events, and generated or uploaded assets.", icon: <Briefcase size={18} /> },
    { id: "health-cost", label: "Health & cost", description: "Watch readiness checks, LLM backend health, token usage, latency, and estimated cost.", icon: <Activity size={18} /> },
    { id: "audit", label: "Audit", description: "Review sensitive operations, high-risk actions, request IDs, user scope, and metadata for investigations.", icon: <FileText size={18} /> },
    { id: "evaluation", label: "Evaluation", description: "Run lightweight evaluations over real runtime data, inspect pass/fail findings, and close review items.", icon: <ShieldCheck size={18} /> }
  ];
  const selectedAdminSection = adminSections.find((section) => section.id === adminSection) || adminSections[0];
  const selectedSkill = skills.find((skill) => skill.name === selectedName) || null;
  const reviewIssues = review?.issues || [];
  const skillTabs: Array<AdminTabOption<typeof skillTab>> = [
    { id: "overview", label: "Overview", icon: <Info size={15} /> },
    { id: "review", label: "Review", icon: <ShieldCheck size={15} />, count: reviewIssues.length },
    { id: "executions", label: "Executions", icon: <Activity size={15} />, count: executions.length },
    { id: "versions", label: "Versions", icon: <Archive size={15} />, count: versions.length }
  ];
  const filteredSkills = useMemo(() => skills.filter((skill) => {
    const statusMatches = statusFilter === "all" || skill.status === statusFilter;
    return statusMatches && fuzzyMatch(query, [
      skill.name,
      skill.display_name,
      skill.description,
      skill.category,
      skill.status,
      skill.version,
      skill.source
    ]);
  }).sort(compareSkills), [skills, query, statusFilter]);

  const loadSkills = async () => {
    if (!token) {
      setError("Enter the admin token to load the console.");
      setSkills([]);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const next = await api.adminSkills(token);
      setSkills(next);
      setSelectedName((current) => {
        if (current && next.some((skill) => skill.name === current)) return current;
        return next[0]?.name || "";
      });
      setNotice(`Loaded ${next.length} skills`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const loadSkillDetails = async (name: string) => {
    if (!token || !name) return;
    setDetailsLoading(true);
    setError("");
    try {
      const [nextReview, nextVersions, nextSummary, nextExecutions] = await Promise.all([
        api.adminSkillReview(name, token),
        api.adminSkillVersions(name, token),
        api.adminSkillAnalytics(name, token),
        api.adminSkillExecutions(name, token, 20)
      ]);
      setReview(nextReview);
      setVersions(nextVersions);
      setSummary(nextSummary);
      setExecutions(nextExecutions);
    } catch (err) {
      setError(errorMessage(err));
      setReview(null);
      setVersions([]);
      setSummary(null);
      setExecutions([]);
    } finally {
      setDetailsLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadSkills();
  }, []);

  useEffect(() => {
    if (selectedName && token) void loadSkillDetails(selectedName);
  }, [selectedName, token]);

  const refreshSelected = async () => {
    await loadSkills();
    if (selectedName) await loadSkillDetails(selectedName);
  };

  const changeSkillStatus = async (action: "publish" | "unpublish" | "disable") => {
    if (!selectedSkill || !token) return;
    setActionBusy(action);
    setError("");
    try {
      const updated = await api.setAdminSkillStatus(selectedSkill.name, token, action);
      setSkills((current) => current.map((skill) => skill.name === updated.name ? updated : skill));
      setNotice(`/${updated.name} ${action} complete`);
      await loadSkillDetails(updated.name);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const policySaved = (updated: AdminSkill) => {
    setSkills((current) => current.map((skill) => skill.name === updated.name ? updated : skill));
    setPolicyTarget(null);
    setNotice(`Policy saved for /${updated.name}`);
    void loadSkillDetails(updated.name);
  };

  return (
    <main className="admin-shell">
      <aside className="admin-sidebar">
        <div className="admin-brand">
          <BrandLogo />
          <div>
            <strong>AgentAPI Admin</strong>
            <small>{userLabel}</small>
          </div>
        </div>
        <div className="admin-token-box">
          <label>
            Admin token
            <input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
          </label>
          <button className="primary wide" onClick={loadSkills} disabled={loading || !token || adminSection !== "skills"}>
            {loading ? "Loading" : "Load skill data"}
          </button>
        </div>
        <div className="admin-sidebar-actions">
          <button onClick={onExit}><MessageCircle size={16} /> Back to app</button>
          <button onClick={onLogout}><LogOut size={16} /> Log out</button>
        </div>
      </aside>
      <section className="admin-main">
        <header className="admin-header">
          <div>
            <h1>{selectedAdminSection.label}</h1>
            <p>{selectedAdminSection.description}</p>
          </div>
          {adminSection === "skills" && (
            <button className="skill-action" onClick={refreshSelected} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>Refresh</span>
            </button>
          )}
        </header>
        <AdminTabs tabs={adminSections} active={adminSection} onChange={setAdminSection} label="Admin sections" />
        {(error || notice) && (
          <div className={`admin-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        {!token ? (
          <div className="admin-empty">
            <ShieldCheck size={26} />
            <strong>Admin token required</strong>
            <p>Enter `AGENT_API_ADMIN_TOKEN` to load protected admin APIs. This console is separate from the C-end workspace.</p>
          </div>
        ) : adminSection === "users" ? (
          <AdminUsersPanel api={api} adminToken={adminToken} />
        ) : adminSection === "jobs-assets" ? (
          <AdminOpsPanel api={api} adminToken={adminToken} />
        ) : adminSection === "health-cost" ? (
          <AdminHealthCostPanel api={api} adminToken={adminToken} />
        ) : adminSection === "audit" ? (
          <AdminAuditPanel api={api} adminToken={adminToken} />
        ) : adminSection === "evaluation" ? (
          <AdminEvaluationPanel api={api} adminToken={adminToken} />
        ) : (
          <div className="admin-skill-layout">
            <section className="admin-list-panel">
              <div className="admin-list-tools">
                <div className="admin-search">
                  <Search size={16} />
                  <input value={query} onChange={(event) => setQuery(event.currentTarget.value)} placeholder="Search skills" aria-label="Search admin skills" />
                </div>
                <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter skill status">
                  <option value="all">All status</option>
                  <option value="published">Published</option>
                  <option value="unpublished">Unpublished</option>
                  <option value="draft">Draft</option>
                  <option value="disabled">Disabled</option>
                  <option value="archived">Archived</option>
                </select>
              </div>
              <div className="admin-skill-list">
                {filteredSkills.map((skill) => (
                  <button
                    key={skill.name}
                    className={`admin-skill-row ${skill.name === selectedName ? "active" : ""}`}
                    onClick={() => setSelectedName(skill.name)}
                  >
                    <SkillGlyph skill={skill} />
                    <span>
                      <strong>{skill.display_name || skill.name}</strong>
                      <small>/{skill.name}</small>
                    </span>
                    <StatusBadge value={skill.status || "unknown"} />
                  </button>
                ))}
                {!filteredSkills.length && <div className="empty-small">{loading ? "Loading..." : "No skills"}</div>}
              </div>
            </section>
            <section className="admin-detail-panel">
              {!selectedSkill ? (
                <div className="admin-empty">
                  <Sparkles size={24} />
                  <strong>Select a skill</strong>
                  <p>Choose a registry skill to inspect release status, policy, review issues, and execution metrics.</p>
                </div>
              ) : (
                <>
                  <div className="admin-skill-head">
                    <div className="skill-modal-heading">
                      <SkillGlyph skill={selectedSkill} />
                      <div>
                        <h2>{selectedSkill.display_name || selectedSkill.name}</h2>
                        <small>/{selectedSkill.name} · {selectedSkill.source || "registry"} · {selectedSkill.version ? `v${selectedSkill.version}` : "unversioned"}</small>
                      </div>
                    </div>
                    <StatusBadge value={selectedSkill.status || "unknown"} />
                  </div>
                  <p className="admin-description">{selectedSkill.description || "No description available."}</p>
                  <div className="admin-action-row">
                    <button className="primary skill-action" onClick={() => changeSkillStatus("publish")} disabled={Boolean(actionBusy)}>
                      <PlayCircle size={16} />
                      <span>{actionBusy === "publish" ? "Publishing" : "Publish"}</span>
                    </button>
                    <button className="skill-action" onClick={() => changeSkillStatus("unpublish")} disabled={Boolean(actionBusy)}>
                      <Archive size={16} />
                      <span>{actionBusy === "unpublish" ? "Unpublishing" : "Unpublish"}</span>
                    </button>
                    <button className="skill-action danger-outline" onClick={() => changeSkillStatus("disable")} disabled={Boolean(actionBusy)}>
                      <UserX size={16} />
                      <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
                    </button>
                    <button className="skill-action" onClick={() => setPolicyTarget(selectedSkill)}>
                      <ShieldCheck size={16} />
                      <span>Policy</span>
                    </button>
                    <button className="skill-action" onClick={() => loadSkillDetails(selectedSkill.name)} disabled={detailsLoading}>
                      <RefreshCw size={16} />
                      <span>{detailsLoading ? "Loading" : "Review"}</span>
                    </button>
                  </div>
                  <div className="admin-metrics">
                    <AdminMetric label="Runs" value={String(summary?.total ?? 0)} />
                    <AdminMetric label="Failure rate" value={formatPercent(summary?.failure_rate ?? 0)} />
                    <AdminMetric label="Avg latency" value={`${summary?.average_latency_ms ?? 0} ms`} />
                    <AdminMetric label="Versions" value={String(versions.length)} />
                  </div>
                  <AdminTabs tabs={skillTabs} active={skillTab} onChange={setSkillTab} label="Skill detail sections" compact />
                  <div className="admin-detail-grid">
                    {skillTab === "overview" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Registry</h3>
                        </div>
                        <div className="admin-facts">
                          <SkillFact label="Category" value={selectedSkill.category || "General"} />
                          <SkillFact label="Root" value={selectedSkill.skill_root || "Not set"} />
                          <SkillFact label="Hash" value={selectedSkill.content_hash ? selectedSkill.content_hash.slice(0, 12) : "Not set"} />
                          <SkillFact label="Updated" value={formatTime(selectedSkill.updated_at || selectedSkill.created_at || "")} />
                        </div>
                      </section>
                    )}
                    {skillTab === "review" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Review</h3>
                          {review && <StatusBadge value={review.passed ? "passed" : "blocked"} />}
                        </div>
                        {!review && <p className="muted-text">No review loaded.</p>}
                        {review && !reviewIssues.length && <p className="muted-text">No blocking issues or warnings.</p>}
                        {reviewIssues.map((issue) => (
                          <div key={`${issue.code}-${issue.field}`} className={`review-issue ${issue.severity}`}>
                            <strong>{issue.code}</strong>
                            <span>{issue.message}</span>
                            {issue.field && <small>{issue.field}</small>}
                          </div>
                        ))}
                      </section>
                    )}
                    {skillTab === "executions" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Recent executions</h3>
                        </div>
                        <div className="admin-table">
                          {executions.slice(0, 12).map((execution) => (
                            <div key={execution.id} className="admin-table-row">
                              <StatusBadge value={execution.status} />
                              <span>{execution.duration_ms} ms</span>
                              {(execution.provider || execution.model) && <span>{[execution.provider, execution.model].filter(Boolean).join(" / ")}</span>}
                              {execution.error_kind && <span>{execution.error_kind}</span>}
                              {typeof execution.artifact_count === "number" && execution.artifact_count > 0 && <span>{execution.artifact_count} artifact{execution.artifact_count === 1 ? "" : "s"}</span>}
                              <small>{formatTime(execution.completed_at)}</small>
                              {execution.error && <em>{execution.error}</em>}
                              {execution.input_summary && <em>{execution.input_summary}</em>}
                            </div>
                          ))}
                          {!executions.length && <p className="muted-text">No executions recorded.</p>}
                        </div>
                      </section>
                    )}
                    {skillTab === "versions" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Versions</h3>
                        </div>
                        <div className="admin-table">
                          {versions.slice(0, 12).map((version) => (
                            <div key={`${version.version}-${version.content_hash}-${version.created_at}`} className="admin-table-row">
                              <strong>{version.version || "unversioned"}</strong>
                              <span>{version.content_hash ? version.content_hash.slice(0, 10) : "no hash"}</span>
                              <small>{formatTime(version.published_at || version.created_at)}</small>
                              {version.changelog && <em>{version.changelog}</em>}
                            </div>
                          ))}
                          {!versions.length && <p className="muted-text">No versions recorded.</p>}
                        </div>
                      </section>
                    )}
                  </div>
                </>
              )}
            </section>
          </div>
        )}
      </section>
      {policyTarget && (
        <SkillPolicyModal
          api={api}
          skill={policyTarget}
          adminToken={adminToken}
          onAdminTokenChange={onAdminTokenChange}
          onSaved={policySaved}
          onClose={() => setPolicyTarget(null)}
        />
      )}
    </main>
  );
}

function AdminUsersPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [loading, setLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [userTab, setUserTab] = useState<"account" | "access">("account");
  const token = adminToken.trim();
  const selectedUser = users.find((user) => user.id === selectedID) || null;
  const userTabs: Array<AdminTabOption<typeof userTab>> = [
    { id: "account", label: "Account", icon: <Database size={15} /> },
    { id: "access", label: "Access", icon: <ShieldCheck size={15} /> }
  ];

  const loadUsers = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const next = await api.adminUsers(token, { q: query, status: statusFilter, limit: 100 });
      setUsers(next);
      setSelectedID((current) => {
        if (current && next.some((user) => user.id === current)) return current;
        return next[0]?.id || "";
      });
      setNotice(`Loaded ${next.length} users`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadUsers();
  }, [token]);

  const updateUser = (updated: AdminUser) => {
    setUsers((current) => current.map((user) => user.id === updated.id ? updated : user));
    setSelectedID(updated.id);
  };

  const runAction = async (action: "disable" | "ban" | "reactivate") => {
    if (!selectedUser || !token) return;
    setActionBusy(action);
    setError("");
    try {
      const updated = await api.adminUserAction(selectedUser.id, token, action);
      updateUser(updated);
      setNotice(`${selectedUser.email} ${action} complete`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const patchStatus = async (status: "active" | "disabled" | "banned") => {
    if (!selectedUser || !token || selectedUser.status === status) return;
    setActionBusy(status);
    setError("");
    try {
      const updated = await api.updateAdminUserStatus(selectedUser.id, token, status);
      updateUser(updated);
      setNotice(`${selectedUser.email} status set to ${status}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <div className="admin-search">
            <Search size={16} />
            <input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadUsers();
              }}
              placeholder="Search users"
              aria-label="Search admin users"
            />
          </div>
          <div className="admin-filter-row">
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter user status">
              <option value="all">All status</option>
              <option value="active">Active</option>
              <option value="disabled">Disabled</option>
              <option value="banned">Banned</option>
            </select>
            <button className="skill-action" onClick={loadUsers} disabled={loading}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Search"}</span>
            </button>
          </div>
        </div>
        <div className="admin-skill-list">
          {users.map((user) => (
            <button
              key={user.id}
              className={`admin-skill-row ${user.id === selectedID ? "active" : ""}`}
              onClick={() => setSelectedID(user.id)}
            >
              <span className="user-avatar">{initials(user.display_name || user.email)}</span>
              <span>
                <strong>{user.display_name || user.email}</strong>
                <small>{user.email}</small>
              </span>
              <StatusBadge value={user.status || "unknown"} />
            </button>
          ))}
          {!users.length && <div className="empty-small">{loading ? "Loading..." : "No users"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        {!selectedUser ? (
          <div className="admin-empty">
            <Database size={24} />
            <strong>Select a user</strong>
            <p>Choose a user to inspect account status, refresh-token state, and access controls.</p>
          </div>
        ) : (
          <>
            <div className="admin-skill-head">
              <div className="skill-modal-heading">
                <span className="user-avatar large">{initials(selectedUser.display_name || selectedUser.email)}</span>
                <div>
                  <h2>{selectedUser.display_name || selectedUser.email}</h2>
                  <small>{selectedUser.email}</small>
                </div>
              </div>
              <StatusBadge value={selectedUser.status || "unknown"} />
            </div>
            <div className="admin-action-row">
              <button className="primary skill-action" onClick={() => runAction("reactivate")} disabled={Boolean(actionBusy) || selectedUser.status === "active"}>
                <PlayCircle size={16} />
                <span>{actionBusy === "reactivate" ? "Reactivating" : "Reactivate"}</span>
              </button>
              <button className="skill-action" onClick={() => runAction("disable")} disabled={Boolean(actionBusy) || selectedUser.status === "disabled"}>
                <UserX size={16} />
                <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
              </button>
              <button className="skill-action danger-outline" onClick={() => runAction("ban")} disabled={Boolean(actionBusy) || selectedUser.status === "banned"}>
                <UserX size={16} />
                <span>{actionBusy === "ban" ? "Banning" : "Ban"}</span>
              </button>
              <select
                className="admin-status-select"
                value={selectedUser.status}
                onChange={(event) => patchStatus(event.currentTarget.value as "active" | "disabled" | "banned")}
                aria-label="Set user status"
                disabled={Boolean(actionBusy)}
              >
                <option value="active">active</option>
                <option value="disabled">disabled</option>
                <option value="banned">banned</option>
              </select>
            </div>
            <div className="admin-metrics">
              <AdminMetric label="Refresh tokens" value={String(selectedUser.refresh_token_count || 0)} />
              <AdminMetric label="Active tokens" value={String(selectedUser.active_refresh_token_count || 0)} />
              <AdminMetric label="Created" value={formatShortDate(selectedUser.created_at)} />
              <AdminMetric label="Last login" value={formatShortDate(selectedUser.last_login_at)} />
            </div>
            <AdminTabs tabs={userTabs} active={userTab} onChange={setUserTab} label="User detail sections" compact />
            <div className="admin-detail-grid">
              {userTab === "account" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Account</h3>
                </div>
                <div className="admin-facts">
                  <SkillFact label="User ID" value={selectedUser.id} />
                  <SkillFact label="Email" value={selectedUser.email} />
                  <SkillFact label="Display name" value={selectedUser.display_name || "Not set"} />
                  <SkillFact label="Updated" value={formatTime(selectedUser.updated_at)} />
                </div>
              </section>}
              {userTab === "access" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Access notes</h3>
                </div>
                <p className="muted-text">Disabled and banned users cannot log in or refresh tokens. Changing a user to an inactive status revokes existing refresh tokens immediately; access tokens expire on their normal short TTL.</p>
              </section>}
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function AdminOpsPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [userID, setUserID] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [assetKind, setAssetKind] = useState("all");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [assets, setAssets] = useState<Asset[]>([]);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [selectedSessionID, setSelectedSessionID] = useState("");
  const [selectedJobID, setSelectedJobID] = useState("");
  const [loading, setLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [opsTab, setOpsTab] = useState<"session" | "jobs" | "events" | "assets">("jobs");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedSession = sessions.find((session) => session.id === selectedSessionID) || null;
  const selectedJob = jobs.find((job) => job.id === selectedJobID) || null;
  const opsTabs: Array<AdminTabOption<typeof opsTab>> = [
    { id: "session", label: "Session", icon: <MessageCircle size={15} />, count: sessions.length },
    { id: "jobs", label: "Jobs", icon: <Briefcase size={15} />, count: jobs.length },
    { id: "events", label: "Events", icon: <Activity size={15} />, count: events.length },
    { id: "assets", label: "Assets", icon: <FileUp size={15} />, count: assets.length }
  ];

  const loadOps = async (sessionID = selectedSessionID, jobID = selectedJobID) => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID to inspect sessions, jobs, and assets.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const [nextSessions, nextJobs, nextAssets] = await Promise.all([
        api.adminOpsSessions(token, cleanUserID, { q: query, limit: 100 }),
        api.adminOpsJobs(token, cleanUserID, { sessionId: sessionID, q: query, status: statusFilter, limit: 100 }),
        api.adminOpsAssets(token, cleanUserID, { sessionId: sessionID, jobId: jobID, q: query, kind: assetKind, limit: 100 })
      ]);
      setSessions(nextSessions);
      setJobs(nextJobs);
      setAssets(nextAssets);
      const nextSessionID = sessionID && nextSessions.some((session) => session.id === sessionID) ? sessionID : "";
      const nextJobID = jobID && nextJobs.some((job) => job.id === jobID) ? jobID : nextJobs[0]?.id || "";
      setSelectedSessionID(nextSessionID);
      setSelectedJobID(nextJobID);
      if (nextJobID) {
        const nextEvents = await api.adminOpsJobEvents(token, cleanUserID, nextJobID, 500);
        setEvents(nextEvents);
      } else {
        setEvents([]);
      }
      setNotice(`Loaded ${nextSessions.length} sessions, ${nextJobs.length} jobs, ${nextAssets.length} assets`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const openSession = async (sessionID: string) => {
    setSelectedSessionID(sessionID);
    setSelectedJobID("");
    setEvents([]);
    await loadOps(sessionID, "");
  };

  const openJob = async (jobID: string) => {
    setSelectedJobID(jobID);
    if (!token || !cleanUserID) return;
    setError("");
    try {
      setEvents(await api.adminOpsJobEvents(token, cleanUserID, jobID, 500));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const cancelJob = async () => {
    if (!selectedJob || !token || !cleanUserID) return;
    setActionBusy("cancel");
    setError("");
    try {
      await api.adminOpsCancelJob(token, cleanUserID, selectedJob.id);
      setNotice(`Cancelled ${selectedJob.id}`);
      await loadOps(selectedSessionID, selectedJob.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID</span>
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="user_id" aria-label="Troubleshooting user ID" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadOps();
              }}
              placeholder="Search IDs, content, filenames"
              aria-label="Search troubleshooting data"
            />
          </div>
          <div className="admin-filter-row">
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter job status">
              <option value="all">All jobs</option>
              <option value="queued">Queued</option>
              <option value="running">Running</option>
              <option value="succeeded">Succeeded</option>
              <option value="failed">Failed</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <select value={assetKind} onChange={(event) => setAssetKind(event.currentTarget.value)} aria-label="Filter asset kind">
              <option value="all">All assets</option>
              <option value="attachment">Attachments</option>
              <option value="artifact">Artifacts</option>
            </select>
          </div>
          <button className="primary wide" onClick={() => loadOps()} disabled={loading || !token || !cleanUserID}>
            {loading ? "Loading" : "Load troubleshooting data"}
          </button>
        </div>
        <div className="admin-skill-list">
          {sessions.map((session) => (
            <button key={session.id} className={`admin-skill-row ${session.id === selectedSessionID ? "active" : ""}`} onClick={() => openSession(session.id)}>
              <MessageCircle size={18} />
              <span>
                <strong>{sessionTitle(session)}</strong>
                <small>{session.id}</small>
              </span>
              <small>{(session.messages || []).filter((message) => !message.hidden).length}</small>
            </button>
          ))}
          {!sessions.length && <div className="empty-small">{loading ? "Loading..." : "No sessions"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        {!cleanUserID ? (
          <div className="admin-empty">
            <Briefcase size={24} />
            <strong>Enter a user ID</strong>
            <p>Use User Management to copy a user ID, then inspect their sessions, jobs, replay events, and assets here.</p>
          </div>
        ) : (
          <>
            <div className="admin-skill-head">
              <div>
                <h2>{selectedSession ? sessionTitle(selectedSession) : "User scope"}</h2>
                <small>{selectedSessionID || cleanUserID}</small>
              </div>
              <button className="skill-action" onClick={() => loadOps()} disabled={loading}>
                <RefreshCw size={16} />
                <span>{loading ? "Loading" : "Refresh"}</span>
              </button>
            </div>
            <div className="admin-metrics">
              <AdminMetric label="Sessions" value={String(sessions.length)} />
              <AdminMetric label="Jobs" value={String(jobs.length)} />
              <AdminMetric label="Assets" value={String(assets.length)} />
              <AdminMetric label="Events" value={String(events.length)} />
            </div>
            <AdminTabs tabs={opsTabs} active={opsTab} onChange={setOpsTab} label="Troubleshooting sections" compact />
            <div className="admin-detail-grid">
              {opsTab === "session" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Selected session</h3>
                </div>
                <div className="admin-facts">
                  <SkillFact label="Session ID" value={selectedSessionID || "All sessions"} />
                  <SkillFact label="Messages" value={String((selectedSession?.messages || []).filter((message) => !message.hidden).length)} />
                  <SkillFact label="Working dir" value={selectedSession?.working_dir || "Not selected"} />
                  <SkillFact label="Updated" value={formatTime(selectedSession?.updated_at || "")} />
                </div>
              </section>}
              {opsTab === "jobs" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Jobs</h3>
                  {selectedJob && <StatusBadge value={selectedJob.status} />}
                </div>
                <div className="admin-table">
                  {jobs.slice(0, 12).map((job) => (
                    <button key={job.id} className={`admin-table-row button-row ${job.id === selectedJobID ? "active" : ""}`} onClick={() => openJob(job.id)}>
                      <StatusBadge value={job.status} />
                      <span>{job.type || "chat"}</span>
                      <small>{job.id}</small>
                      {job.error && <em>{job.error}</em>}
                    </button>
                  ))}
                  {!jobs.length && <p className="muted-text">No jobs found.</p>}
                </div>
                {selectedJob && (
                  <div className="admin-action-row">
                    <button className="skill-action danger-outline" onClick={cancelJob} disabled={Boolean(actionBusy) || terminalJobs.has(selectedJob.status)}>
                      <Square size={15} />
                      <span>{actionBusy === "cancel" ? "Cancelling" : "Cancel job"}</span>
                    </button>
                  </div>
                )}
              </section>}
              {opsTab === "events" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Job events</h3>
                </div>
                <div className="admin-table">
                  {events.slice(0, 12).map((event) => (
                    <div key={event.id} className="admin-table-row">
                      <StatusBadge value={event.type} />
                      <span>{event.event?.content || event.event?.error || event.event?.type || "event"}</span>
                      <small>{formatTime(event.created_at)}</small>
                    </div>
                  ))}
                  {!events.length && <p className="muted-text">Select a job to inspect replay events.</p>}
                </div>
              </section>}
              {opsTab === "assets" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Assets</h3>
                </div>
                <div className="admin-table">
                  {assets.slice(0, 12).map((asset) => (
                    <div key={asset.id} className="admin-table-row">
                      <StatusBadge value={asset.kind} />
                      <span>{asset.filename}</span>
                      <small>{formatBytes(asset.size_bytes)} · {asset.id}</small>
                      {asset.job_id && <em>{asset.job_id}</em>}
                    </div>
                  ))}
                  {!assets.length && <p className="muted-text">No attachments or artifacts found.</p>}
                </div>
              </section>}
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function AdminAuditPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [audit, setAudit] = useState<AuditLogSummary | null>(null);
  const [risk, setRisk] = useState<RiskSummary | null>(null);
  const [reviews, setReviews] = useState<RiskReviewSummary | null>(null);
  const [selectedID, setSelectedID] = useState("");
  const [selectedRiskID, setSelectedRiskID] = useState("");
  const [userID, setUserID] = useState("");
  const [query, setQuery] = useState("");
  const [eventFilter, setEventFilter] = useState("all");
  const [operationFilter, setOperationFilter] = useState("all");
  const [riskFilter, setRiskFilter] = useState("all");
  const [reviewStatusFilter, setReviewStatusFilter] = useState("pending");
  const [days, setDays] = useState(7);
  const [loading, setLoading] = useState(false);
  const [reviewBusy, setReviewBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [auditTab, setAuditTab] = useState<"overview" | "reviews" | "audit-event" | "risk-event">("overview");
  const token = adminToken.trim();
  const records = audit?.records || [];
  const riskEvents = risk?.events || [];
  const reviewItems = reviews?.items || [];
  const selected = records.find((record) => record.id === selectedID) || records[0] || null;
  const selectedRisk = riskEvents.find((event) => event.id === selectedRiskID) || riskEvents[0] || null;
  const auditTabs: Array<AdminTabOption<typeof auditTab>> = [
    { id: "overview", label: "Overview", icon: <Activity size={15} />, count: audit?.total ?? 0 },
    { id: "reviews", label: "Reviews", icon: <ShieldCheck size={15} />, count: reviews?.pending ?? 0 },
    { id: "audit-event", label: "Audit event", icon: <FileText size={15} />, count: records.length },
    { id: "risk-event", label: "Risk event", icon: <AlertCircle size={15} />, count: riskEvents.length }
  ];

  const loadAudit = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const [next, nextRisk, nextReviews] = await Promise.all([
        api.adminOpsAudit(token, {
          userId: userID.trim(),
          event: eventFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 300
        }),
        api.adminOpsRisk(token, {
          userId: userID.trim(),
          operation: operationFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 300
        }),
        api.adminOpsRiskReviews(token, {
          userId: userID.trim(),
          status: reviewStatusFilter,
          operation: operationFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 100
        })
      ]);
      setAudit(next);
      setRisk(nextRisk);
      setReviews(nextReviews);
      setSelectedID((current) => {
        if (current && next.records.some((record) => record.id === current)) return current;
        return next.records[0]?.id || "";
      });
      setSelectedRiskID((current) => {
        if (current && nextRisk.events.some((event) => event.id === current)) return current;
        return nextRisk.events[0]?.id || "";
      });
      setNotice(`Loaded ${next.total} audit events, ${nextRisk.total} risk events, and ${nextReviews.total} reviews`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadAudit();
  }, [token]);

  const eventOptions = useMemo(() => {
    const values = new Set<string>();
    audit?.by_event?.forEach((group) => values.add(group.key));
    records.forEach((record) => values.add(record.event));
    return Array.from(values).sort();
  }, [audit, records]);

  const operationOptions = useMemo(() => {
    const values = new Set<string>();
    risk?.by_operation?.forEach((group) => values.add(group.key));
    riskEvents.forEach((event) => values.add(event.operation));
    return Array.from(values).sort();
  }, [risk, riskEvents]);

  const updateReview = async (id: string, status: string, resolution = "") => {
    if (!token) return;
    setReviewBusy(id);
    setError("");
    try {
      await api.updateRiskReview(token, id, {
        status,
        assignedTo: status === "in_review" ? "admin" : "",
        resolution,
        note: resolution || status
      });
      await loadAudit();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setReviewBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID filter</span>
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="Audit user ID filter" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadAudit();
              }}
              placeholder="Search request, event, metadata"
              aria-label="Search audit logs"
            />
          </div>
          <div className="admin-filter-row">
            <select value={riskFilter} onChange={(event) => setRiskFilter(event.currentTarget.value)} aria-label="Filter audit risk">
              <option value="all">All risks</option>
              <option value="high">High risk</option>
              <option value="medium">Medium risk</option>
              <option value="low">Low risk</option>
            </select>
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Audit time range">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
          </div>
          <select value={eventFilter} onChange={(event) => setEventFilter(event.currentTarget.value)} aria-label="Filter audit event">
            <option value="all">All events</option>
            {eventOptions.map((event) => <option key={event} value={event}>{event}</option>)}
          </select>
          <select value={operationFilter} onChange={(event) => setOperationFilter(event.currentTarget.value)} aria-label="Filter risk operation">
            <option value="all">All operations</option>
            {operationOptions.map((operation) => <option key={operation} value={operation}>{operation}</option>)}
          </select>
          <select value={reviewStatusFilter} onChange={(event) => setReviewStatusFilter(event.currentTarget.value)} aria-label="Filter risk reviews">
            <option value="pending">Pending reviews</option>
            <option value="in_review">In review</option>
            <option value="resolved">Resolved</option>
            <option value="dismissed">Dismissed</option>
            <option value="all">All reviews</option>
          </select>
          <button className="primary wide" onClick={loadAudit} disabled={loading || !token}>
            {loading ? "Loading" : "Load audit logs"}
          </button>
        </div>
        <div className="admin-skill-list">
          {records.map((record) => (
            <button key={record.id} className={`admin-skill-row ${record.id === selected?.id ? "active" : ""}`} onClick={() => setSelectedID(record.id)}>
              <FileText size={18} />
              <span>
                <strong>{record.event}</strong>
                <small>{auditRecordSummary(record)}</small>
              </span>
              <StatusBadge value={record.risk_level || "low"} />
            </button>
          ))}
          {!records.length && <div className="empty-small">{loading ? "Loading..." : "No audit events"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>Risk overview</h2>
            <small>{audit?.since ? `Since ${formatTime(audit.since)}` : "No audit window loaded"}</small>
          </div>
          <button className="skill-action" onClick={loadAudit} disabled={loading || !token}>
            <RefreshCw size={16} />
            <span>{loading ? "Loading" : "Refresh"}</span>
          </button>
        </div>
        <div className="admin-metrics">
          <AdminMetric label="Events" value={String(audit?.total ?? 0)} />
          <AdminMetric label="Risk events" value={String(risk?.total ?? 0)} />
          <AdminMetric label="High risk" value={String((audit?.high_risk ?? 0) + (risk?.high_risk ?? 0))} />
          <AdminMetric label="Pending reviews" value={String(reviews?.pending ?? 0)} />
          <AdminMetric label="Risk scores" value={String(risk?.scores?.length ?? 0)} />
        </div>
        <AdminTabs tabs={auditTabs} active={auditTab} onChange={setAuditTab} label="Audit detail sections" compact />
        <div className="admin-detail-grid">
          {auditTab === "reviews" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Manual review queue</h3>
              <StatusBadge value={reviewStatusFilter} />
            </div>
            <div className="admin-table">
              {reviewItems.slice(0, 12).map((item) => (
                <div key={item.id} className="admin-table-row">
                  <StatusBadge value={item.priority || item.risk_level || "low"} />
                  <span>
                    <strong>{item.operation}</strong>
                    <small>{item.reason} · {item.user_id || item.ip_address || "anonymous"}</small>
                  </span>
                  <small>{formatTime(item.updated_at)}</small>
                  <button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "in_review")}>Review</button>
                  <button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "resolved", "resolved by admin")}>Resolve</button>
                  <button className="small danger" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "dismissed", "dismissed by admin")}>Dismiss</button>
                </div>
              ))}
              {!reviewItems.length && <p className="muted-text">No manual review items in this filter.</p>}
            </div>
          </section>}
          {auditTab === "overview" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Event mix</h3>
            </div>
            <div className="admin-table">
              {(audit?.by_event || []).slice(0, 12).map((group) => (
                <button key={group.key} className="admin-table-row button-row" onClick={() => setEventFilter(group.key)}>
                  <StatusBadge value={auditRiskForEventName(group.key)} />
                  <span>{group.key}</span>
                  <small>{group.count} events</small>
                </button>
              ))}
              {!audit?.by_event?.length && <p className="muted-text">No events in this window.</p>}
            </div>
          </section>}
          {auditTab === "overview" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Risk scores</h3>
            </div>
            <div className="admin-table">
              {(risk?.scores || []).slice(0, 10).map((score) => (
                <button key={`${score.subject_type}:${score.subject_id}`} className="admin-table-row button-row" onClick={() => score.subject_type === "user" ? setUserID(score.subject_id) : undefined}>
                  <StatusBadge value={score.risk_level || "low"} />
                  <span>{score.subject_type}:{score.subject_id}</span>
                  <small>{score.score} score · {score.event_count} events</small>
                </button>
              ))}
              {!risk?.scores?.length && <p className="muted-text">No accumulated risk scores.</p>}
            </div>
          </section>}
          {auditTab === "audit-event" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Selected audit event</h3>
              {selected && <StatusBadge value={selected.risk_level || "low"} />}
            </div>
            {selected ? (
              <div className="admin-facts">
                <SkillFact label="Event" value={selected.event} />
                <SkillFact label="User ID" value={selected.user_id || "system"} />
                <SkillFact label="Request ID" value={selected.request_id || "none"} />
                <SkillFact label="Created" value={formatTime(selected.created_at)} />
                <SkillFact label="IP" value={selected.ip_address || "unknown"} />
                <SkillFact label="Session" value={selected.session_id || "none"} />
                <SkillFact label="Job" value={selected.job_id || "none"} />
                <SkillFact label="Asset" value={selected.asset_id || "none"} />
              </div>
            ) : (
              <p className="muted-text">Select an audit event to inspect details.</p>
            )}
          </section>}
          {auditTab === "audit-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Metadata</h3>
            </div>
            <pre className="admin-code-block">{selected ? formatAuditMetadata(selected) : "{}"}</pre>
          </section>}
          {auditTab === "risk-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Risk event queue</h3>
            </div>
            <div className="admin-table">
              {riskEvents.slice(0, 12).map((event) => (
                <button key={event.id} className={`admin-table-row button-row ${event.id === selectedRisk?.id ? "active" : ""}`} onClick={() => setSelectedRiskID(event.id)}>
                  <StatusBadge value={event.risk_level || "low"} />
                  <span>{event.operation}</span>
                  <small>{riskEventSummary(event)}</small>
                  {event.reason && <em>{event.reason}</em>}
                </button>
              ))}
              {!riskEvents.length && <p className="muted-text">No risk events in the current filter.</p>}
            </div>
          </section>}
          {auditTab === "risk-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Selected risk event</h3>
              {selectedRisk && <StatusBadge value={selectedRisk.risk_level || "low"} />}
            </div>
            {selectedRisk ? (
              <div className="admin-facts">
                <SkillFact label="Operation" value={selectedRisk.operation} />
                <SkillFact label="Reason" value={selectedRisk.reason} />
                <SkillFact label="Score delta" value={String(selectedRisk.score_delta)} />
                <SkillFact label="User ID" value={selectedRisk.user_id || "anonymous"} />
                <SkillFact label="IP" value={selectedRisk.ip_address || "unknown"} />
                <SkillFact label="Created" value={formatTime(selectedRisk.created_at)} />
              </div>
            ) : (
              <p className="muted-text">Select a risk event to inspect details.</p>
            )}
            <pre className="admin-code-block">{selectedRisk ? JSON.stringify({ metadata: selectedRisk.metadata || {}, request_id: selectedRisk.request_id || "" }, null, 2) : "{}"}</pre>
          </section>}
        </div>
      </section>
    </div>
  );
}

function AdminEvaluationPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
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
  const [resultStatusFilter, setResultStatusFilter] = useState("failed");
  const [days, setDays] = useState(7);
  const [thresholdDraft, setThresholdDraft] = useState({
    min_success_rate: "0.85",
    max_tool_error_rate: "0.05",
    max_llm_error_rate: "0.05",
    max_high_risk_count: "0",
    max_p95_latency_ms: "10000",
    max_cost_usd: ""
  });
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [exportBusy, setExportBusy] = useState("");
  const [reviewBusy, setReviewBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [evaluationTab, setEvaluationTab] = useState<"results" | "selected" | "reviews" | "io">("results");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedRun = runs.find((run) => run.id === selectedRunID) || runs[0] || null;
  const selectedResult = results.find((result) => result.id === selectedResultID) || results[0] || null;
  const reviewsByResultID = useMemo(() => {
    const map = new Map<string, EvaluationReview[]>();
    reviews.forEach((review) => {
      const list = map.get(review.result_id) || [];
      list.push(review);
      map.set(review.result_id, list);
    });
    return map;
  }, [reviews]);

  const updateThresholdDraft = (key: keyof typeof thresholdDraft, value: string) => {
    setThresholdDraft((current) => ({ ...current, [key]: value }));
  };

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
        },
        thresholds: buildEvaluationThresholds(thresholdDraft)
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

  useEffect(() => {
    if (token) void loadEvaluation();
  }, [token]);

  const selectedResultReviews = selectedResult ? reviewsByResultID.get(selectedResult.id) || [] : [];
  const metrics = summary?.metrics || selectedRun?.metrics || {};
  const evaluationTabs: Array<AdminTabOption<typeof evaluationTab>> = [
    { id: "results", label: "Results", icon: <Activity size={15} />, count: results.length },
    { id: "selected", label: "Selected", icon: <Info size={15} /> },
    { id: "reviews", label: "Reviews", icon: <ShieldCheck size={15} />, count: selectedResultReviews.length },
    { id: "io", label: "I/O", icon: <FileText size={15} /> }
  ];

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel evaluation-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID</span>
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="required for new eval" aria-label="Evaluation user ID" />
          </label>
          <div className="admin-filter-row">
            <select value={subjectType} onChange={(event) => setSubjectType(event.currentTarget.value)} aria-label="Evaluation subject">
              <option value="job">Jobs</option>
              <option value="session">Sessions</option>
              <option value="skill_execution">Skill executions</option>
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
          <label className="admin-field">
            <span>Session ID</span>
            <input value={sessionID} onChange={(event) => setSessionID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation session ID" />
          </label>
          <label className="admin-field">
            <span>Job ID</span>
            <input value={jobID} onChange={(event) => setJobID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation job ID" />
          </label>
          <label className="admin-field">
            <span>Skill / model</span>
            <input value={skillName} onChange={(event) => setSkillName(event.currentTarget.value)} placeholder="skill name" aria-label="Evaluation skill name" />
          </label>
          <div className="admin-filter-row">
            <input value={provider} onChange={(event) => setProvider(event.currentTarget.value)} placeholder="provider" aria-label="Evaluation provider" />
            <input value={model} onChange={(event) => setModel(event.currentTarget.value)} placeholder="model" aria-label="Evaluation model" />
          </div>
          <div className="admin-filter-row">
            <label className="admin-field">
              <span>Min success</span>
              <input inputMode="decimal" value={thresholdDraft.min_success_rate} onChange={(event) => updateThresholdDraft("min_success_rate", event.currentTarget.value)} aria-label="Minimum success rate threshold" />
            </label>
            <label className="admin-field">
              <span>Max tool error</span>
              <input inputMode="decimal" value={thresholdDraft.max_tool_error_rate} onChange={(event) => updateThresholdDraft("max_tool_error_rate", event.currentTarget.value)} aria-label="Maximum tool error rate threshold" />
            </label>
          </div>
          <div className="admin-filter-row">
            <label className="admin-field">
              <span>Max LLM error</span>
              <input inputMode="decimal" value={thresholdDraft.max_llm_error_rate} onChange={(event) => updateThresholdDraft("max_llm_error_rate", event.currentTarget.value)} aria-label="Maximum LLM error rate threshold" />
            </label>
            <label className="admin-field">
              <span>Max high risk</span>
              <input inputMode="numeric" value={thresholdDraft.max_high_risk_count} onChange={(event) => updateThresholdDraft("max_high_risk_count", event.currentTarget.value)} aria-label="Maximum high risk count threshold" />
            </label>
          </div>
          <div className="admin-filter-row">
            <label className="admin-field">
              <span>Max P95 ms</span>
              <input inputMode="numeric" value={thresholdDraft.max_p95_latency_ms} onChange={(event) => updateThresholdDraft("max_p95_latency_ms", event.currentTarget.value)} aria-label="Maximum P95 latency threshold" />
            </label>
            <label className="admin-field">
              <span>Max cost USD</span>
              <input inputMode="decimal" value={thresholdDraft.max_cost_usd} onChange={(event) => updateThresholdDraft("max_cost_usd", event.currentTarget.value)} placeholder="optional" aria-label="Maximum cost threshold" />
            </label>
          </div>
          <div className="admin-action-row compact">
            <button className="primary skill-action" onClick={createRun} disabled={running || !token || !cleanUserID}>
              <PlayCircle size={16} />
              <span>{running ? "Running" : "Run eval"}</span>
            </button>
            <button className="skill-action" onClick={() => loadEvaluation()} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>{loading ? "Loading" : "Load"}</span>
            </button>
            <button className="skill-action" onClick={exportResultsCSV} disabled={exportBusy === "csv" || !token}>
              <Download size={16} />
              <span>{exportBusy === "csv" ? "Exporting" : "CSV"}</span>
            </button>
            <button className="skill-action" onClick={exportSummaryMarkdown} disabled={exportBusy === "markdown" || !token}>
              <FileText size={16} />
              <span>{exportBusy === "markdown" ? "Exporting" : "Report"}</span>
            </button>
          </div>
        </div>
        <div className="admin-skill-list">
          {runs.map((run) => (
            <button key={run.id} className={`admin-skill-row ${run.id === selectedRun?.id ? "active" : ""}`} onClick={() => openRun(run.id)}>
              <Activity size={18} />
              <span>
                <strong>{run.name}</strong>
                <small>{run.id} · {formatTime(run.completed_at || run.started_at)}</small>
              </span>
              <StatusBadge value={run.threshold_status || run.status} />
            </button>
          ))}
          {!runs.length && <div className="empty-small">{loading ? "Loading..." : "No eval runs"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>{selectedRun?.name || "Evaluation overview"}</h2>
            <small>{selectedRun ? `${selectedRun.id} · ${selectedRun.scope?.user_id || "user scope"}` : "No run selected"}</small>
          </div>
          {selectedRun && <StatusBadge value={selectedRun.threshold_status || selectedRun.status} />}
        </div>
        <div className="admin-metrics">
          <AdminMetric label="Pass rate" value={formatPercent(summary?.pass_rate ?? selectedRunPassRate(selectedRun))} />
          <AdminMetric label="Failed" value={String(summary?.failed ?? selectedRun?.failed ?? 0)} />
          <AdminMetric label="Warning" value={String(summary?.warning ?? selectedRun?.warning ?? 0)} />
          <AdminMetric label="P95 latency" value={`${metricNumber(metrics, "p95_latency_ms")} ms`} />
          <AdminMetric label="Tokens" value={formatNumber(metricNumber(metrics, "total_tokens"))} />
          <AdminMetric label="Cost" value={formatUSD(metricNumber(metrics, "estimated_cost_usd"))} />
          <AdminMetric label="High risk" value={String(metricNumber(metrics, "high_risk_count"))} />
          <AdminMetric label="Threshold failed" value={String(metricNumber(metrics, "threshold_failed_count"))} />
          <AdminMetric label="Reviews" value={String(reviews.filter((review) => review.status === "pending").length)} />
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
                <button key={result.id} className={`admin-table-row button-row ${result.id === selectedResult?.id ? "active" : ""}`} onClick={() => setSelectedResultID(result.id)}>
                  <StatusBadge value={result.status} />
                  <span>
                    <strong>{result.subject_type}:{result.subject_id}</strong>
                    <small>{[result.user_id, result.session_id, result.job_id, result.skill_name].filter(Boolean).join(" · ") || "runtime record"}</small>
                  </span>
                  <small>{formatNumber(Math.round((result.score || 0) * 100))}</small>
                  {(result.findings || []).slice(0, 2).map((finding) => <em key={`${result.id}-${finding.code}`}>{finding.code}: {finding.message}</em>)}
                </button>
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
                  <button className="small ghost" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "passed")}>Pass</button>
                  <button className="small danger" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "ignored")}>Ignore</button>
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
        </div>
      </section>
    </div>
  );
}

function AdminHealthCostPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [health, setHealth] = useState<AdminHealthStatus | null>(null);
  const [usage, setUsage] = useState<LLMUsageAdminSummary | null>(null);
  const [quota, setQuota] = useState<LLMQuotaAdminSummary | null>(null);
  const [configDraft, setConfigDraft] = useState<Record<string, string>>({});
  const [userID, setUserID] = useState("");
  const [days, setDays] = useState(1);
  const [refundRequests, setRefundRequests] = useState("");
  const [refundTokens, setRefundTokens] = useState("");
  const [refundCost, setRefundCost] = useState("");
  const [quotaReason, setQuotaReason] = useState("");
  const [loading, setLoading] = useState(false);
  const [quotaBusy, setQuotaBusy] = useState("");
  const [configBusy, setConfigBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [healthTab, setHealthTab] = useState<"runtime" | "governance" | "usage" | "quota">("runtime");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const readiness = health?.readiness;
  const llm = health?.llm;
  const healthyBackends = (llm?.backends || []).filter((backend) => backend.healthy).length;
  const healthTabs: Array<AdminTabOption<typeof healthTab>> = [
    { id: "runtime", label: "Runtime", icon: <Activity size={15} />, count: readiness?.checks?.length ?? 0 },
    { id: "governance", label: "Governance", icon: <Settings size={15} /> },
    { id: "usage", label: "Usage", icon: <Database size={15} />, count: usage?.requests ?? 0 },
    { id: "quota", label: "Quota", icon: <ShieldCheck size={15} />, count: quota?.recent_adjustments?.length ?? 0 }
  ];

  const loadHealthCost = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const [nextHealth, nextUsage, nextQuota] = await Promise.all([
        api.adminOpsHealth(token),
        api.adminOpsLLMUsage(token, { userId: cleanUserID, days, limit: 200 }),
        cleanUserID ? api.adminOpsQuota(token, cleanUserID, { days: 1, limit: 20 }) : Promise.resolve(null)
      ]);
      setHealth(nextHealth);
      setUsage(nextUsage);
      setQuota(nextQuota);
      setNotice(`Loaded runtime health and ${nextUsage.requests} LLM usage records`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadHealthCost();
  }, [token]);

  useEffect(() => {
    if (llm?.config) setConfigDraft(llmConfigDraftFromConfig(llm.config));
  }, [llm?.config]);

  const updateConfigDraft = (key: keyof LLMGovernanceConfig, value: string) => {
    setConfigDraft((current) => ({ ...current, [key]: value }));
  };

  const saveLLMConfig = async () => {
    if (!token) return;
    let patch: LLMGovernanceConfig;
    try {
      patch = llmConfigFromDraft(configDraft);
    } catch (err) {
      setError(errorMessage(err));
      return;
    }
    setConfigBusy(true);
    setError("");
    try {
      const nextConfig = await api.updateAdminOpsLLMConfig(token, patch);
      setHealth((current) => current ? { ...current, llm: { ...current.llm, config: nextConfig } } : current);
      setConfigDraft(llmConfigDraftFromConfig(nextConfig));
      setNotice("LLM governance config updated");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setConfigBusy(false);
    }
  };

  const resetQuota = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before resetting quota.");
      return;
    }
    setQuotaBusy("reset");
    setError("");
    try {
      const next = await api.adminOpsQuotaReset(token, cleanUserID, quotaReason.trim());
      setQuota(next);
      setNotice(`Daily quota reset for ${cleanUserID}`);
      await loadHealthCost();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setQuotaBusy("");
    }
  };

  const refundQuota = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before applying a refund.");
      return;
    }
    const requestRefund = Math.max(0, Number(refundRequests) || 0);
    const tokenRefund = Math.max(0, Number(refundTokens) || 0);
    const costRefundUSD = Math.max(0, Number(refundCost) || 0);
    if (!requestRefund && !tokenRefund && !costRefundUSD) {
      setError("Enter at least one refund amount.");
      return;
    }
    setQuotaBusy("refund");
    setError("");
    try {
      const next = await api.adminOpsQuotaRefund(token, { userId: cleanUserID, requestRefund, tokenRefund, costRefundUSD, reason: quotaReason.trim() });
      setQuota(next);
      setRefundRequests("");
      setRefundTokens("");
      setRefundCost("");
      setNotice(`Quota refund applied for ${cleanUserID}`);
      await loadHealthCost();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setQuotaBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID filter</span>
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="LLM usage user filter" />
          </label>
          <div className="admin-filter-row">
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Usage time range">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
            <button className="skill-action" onClick={loadHealthCost} disabled={loading || !token}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Refresh"}</span>
            </button>
          </div>
        </div>
        <div className="admin-skill-list">
          {(readiness?.checks || []).map((check) => (
            <div key={check.name} className="admin-skill-row static">
              <Activity size={18} />
              <span>
                <strong>{check.name}</strong>
                <small>{check.error || "Ready"}</small>
              </span>
              <StatusBadge value={check.status} />
            </div>
          ))}
          {!readiness && <div className="empty-small">{loading ? "Loading..." : "No health snapshot loaded"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>Runtime snapshot</h2>
            <small>{usage?.since ? `Since ${formatTime(usage.since)}` : "No usage window loaded"}</small>
          </div>
          <StatusBadge value={readiness?.status || "unknown"} />
        </div>
        <div className="admin-metrics">
          <AdminMetric label="Requests" value={String(usage?.requests ?? 0)} />
          <AdminMetric label="Tokens" value={formatNumber(usage?.total_tokens ?? 0)} />
          <AdminMetric label="Cost" value={formatUSD(usage?.estimated_cost_usd ?? 0)} />
          <AdminMetric label="Avg latency" value={`${Math.round(usage?.average_latency_ms ?? 0)} ms`} />
        </div>
        <AdminTabs tabs={healthTabs} active={healthTab} onChange={setHealthTab} label="Health and cost sections" compact />
        <div className="admin-detail-grid">
          {healthTab === "runtime" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>LLM backends</h3>
              <small>{healthyBackends}/{llm?.backends?.length || 0} healthy</small>
            </div>
            <div className="admin-table">
              {(llm?.backends || []).map((backend) => (
                <div key={`${backend.name}-${backend.model}`} className="admin-table-row">
                  <StatusBadge value={backend.healthy ? "healthy" : "unhealthy"} />
                  <span>{backend.provider} / {backend.model}</span>
                  <small>{backend.consecutive_failures} failures</small>
                  {backend.last_error && <em>{backend.last_error}</em>}
                </div>
              ))}
              {!llm?.backends?.length && <p className="muted-text">No LLM backend status loaded.</p>}
            </div>
          </section>}
          {healthTab === "governance" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Governance config</h3>
              <button className="skill-action" onClick={saveLLMConfig} disabled={configBusy || !token}>
                <Settings size={15} />
                <span>{configBusy ? "Saving" : "Save"}</span>
              </button>
            </div>
            <div className="admin-config-grid">
              <label className="admin-field">
                <span>Model</span>
                <select value={configDraft.model || ""} onChange={(event) => updateConfigDraft("model", event.currentTarget.value)}>
                  {(llm?.config?.allowed_models || []).map((option) => (
                    <option key={option.id} value={option.id}>{option.label}</option>
                  ))}
                  {!llm?.config?.allowed_models?.length && <option value={configDraft.model || ""}>{configDraft.model || "No model loaded"}</option>}
                </select>
              </label>
              <label className="admin-field">
                <span>Vertex location</span>
                <input value={modelOptionLocation(llm?.config, configDraft.model) || configDraft.vertex_location || ""} readOnly aria-label="Selected model Vertex location" />
              </label>
              <label className="admin-field">
                <span>Daily token quota</span>
                <input inputMode="numeric" value={configDraft.daily_token_quota || ""} onChange={(event) => updateConfigDraft("daily_token_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily request quota</span>
                <input inputMode="numeric" value={configDraft.daily_request_quota || ""} onChange={(event) => updateConfigDraft("daily_request_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily cost quota USD</span>
                <input inputMode="decimal" value={configDraft.daily_cost_quota_usd || ""} onChange={(event) => updateConfigDraft("daily_cost_quota_usd", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Max attempts</span>
                <input inputMode="numeric" value={configDraft.max_attempts || ""} onChange={(event) => updateConfigDraft("max_attempts", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="admin-field">
                <span>Chat timeout ms</span>
                <input inputMode="numeric" value={configDraft.chat_timeout_ms || ""} onChange={(event) => updateConfigDraft("chat_timeout_ms", event.currentTarget.value)} placeholder="60000" />
              </label>
              <label className="admin-field">
                <span>Skill timeout ms</span>
                <input inputMode="numeric" value={configDraft.skill_timeout_ms || ""} onChange={(event) => updateConfigDraft("skill_timeout_ms", event.currentTarget.value)} placeholder="90000" />
              </label>
              <label className="admin-field">
                <span>Input cost / 1M</span>
                <input inputMode="decimal" value={configDraft.input_cost_per_million || ""} onChange={(event) => updateConfigDraft("input_cost_per_million", event.currentTarget.value)} placeholder="0.30" />
              </label>
              <label className="admin-field">
                <span>Output cost / 1M</span>
                <input inputMode="decimal" value={configDraft.output_cost_per_million || ""} onChange={(event) => updateConfigDraft("output_cost_per_million", event.currentTarget.value)} placeholder="2.50" />
              </label>
              <label className="admin-field">
                <span>Retry backoff ms</span>
                <input inputMode="numeric" value={configDraft.retry_backoff_ms || ""} onChange={(event) => updateConfigDraft("retry_backoff_ms", event.currentTarget.value)} placeholder="300" />
              </label>
              <label className="admin-field">
                <span>Failure threshold</span>
                <input inputMode="numeric" value={configDraft.failure_threshold || ""} onChange={(event) => updateConfigDraft("failure_threshold", event.currentTarget.value)} placeholder="3" />
              </label>
              <label className="admin-field">
                <span>Circuit cooldown sec</span>
                <input inputMode="numeric" value={configDraft.circuit_cooldown_seconds || ""} onChange={(event) => updateConfigDraft("circuit_cooldown_seconds", event.currentTarget.value)} placeholder="60" />
              </label>
            </div>
          </section>}
          {healthTab === "usage" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Cost by provider</h3>
            </div>
            <div className="admin-table">
              {(usage?.by_provider || []).map((group) => (
                <div key={`${group.provider}-${group.model}-${group.status}`} className="admin-table-row">
                  <StatusBadge value={group.status} />
                  <span>{group.provider} / {group.model}</span>
                  <small>{formatNumber(group.total_tokens)} tokens</small>
                  <em>{formatUSD(group.estimated_cost_usd)} · {group.requests} req</em>
                </div>
              ))}
              {!usage?.by_provider?.length && <p className="muted-text">No usage records in this window.</p>}
            </div>
          </section>}
          {healthTab === "usage" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Recent usage</h3>
            </div>
            <div className="admin-table">
              {(usage?.recent || []).slice(0, 12).map((record) => (
                <div key={record.id} className="admin-table-row">
                  <StatusBadge value={record.status} />
                  <span>{record.provider} / {record.model}</span>
                  <small>{formatNumber(record.total_tokens)} tokens · {record.latency_ms} ms</small>
                  {record.error && <em>{record.error}</em>}
                </div>
              ))}
              {!usage?.recent?.length && <p className="muted-text">No recent usage records.</p>}
            </div>
          </section>}
          {healthTab === "quota" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Quota reset & refund</h3>
              {cleanUserID ? <small>{cleanUserID}</small> : <small>User ID required</small>}
            </div>
            <div className="admin-metrics compact">
              <AdminMetric label="Effective requests" value={String(quota?.effective_usage?.requests ?? 0)} />
              <AdminMetric label="Effective tokens" value={formatNumber(quota?.effective_usage?.total_tokens ?? 0)} />
              <AdminMetric label="Effective cost" value={formatUSD(quota?.effective_usage?.estimated_cost_usd ?? 0)} />
              <AdminMetric label="Adjustments" value={String(quota?.recent_adjustments?.length ?? 0)} />
            </div>
            <div className="admin-quota-tools">
              <label className="admin-field">
                <span>Refund requests</span>
                <input inputMode="numeric" value={refundRequests} onChange={(event) => setRefundRequests(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund tokens</span>
                <input inputMode="numeric" value={refundTokens} onChange={(event) => setRefundTokens(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund cost USD</span>
                <input inputMode="decimal" value={refundCost} onChange={(event) => setRefundCost(event.currentTarget.value)} placeholder="0.00" />
              </label>
              <label className="admin-field">
                <span>Reason</span>
                <input value={quotaReason} onChange={(event) => setQuotaReason(event.currentTarget.value)} placeholder="support note" />
              </label>
            </div>
            <div className="admin-action-row">
              <button className="skill-action" onClick={refundQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <Download size={15} />
                <span>{quotaBusy === "refund" ? "Applying" : "Apply refund"}</span>
              </button>
              <button className="skill-action danger-outline" onClick={resetQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <RefreshCw size={15} />
                <span>{quotaBusy === "reset" ? "Resetting" : "Reset daily quota"}</span>
              </button>
            </div>
            <div className="admin-table">
              {(quota?.recent_adjustments || []).slice(0, 8).map((adjustment) => (
                <div key={adjustment.id} className="admin-table-row">
                  <StatusBadge value={adjustment.total_token_delta < 0 || adjustment.request_delta < 0 || adjustment.estimated_cost_delta_usd < 0 ? "refund" : "adjust"} />
                  <span>{adjustment.reason || "manual adjustment"}</span>
                  <small>{formatNumber(adjustment.total_token_delta)} tokens · {formatUSD(adjustment.estimated_cost_delta_usd)}</small>
                  <em>{formatTime(adjustment.created_at)}</em>
                </div>
              ))}
              {!quota?.recent_adjustments?.length && <p className="muted-text">{cleanUserID ? "No quota adjustments for this user today." : "Enter a user ID to load quota tools."}</p>}
            </div>
          </section>}
        </div>
      </section>
    </div>
  );
}

function StatusBadge({ value }: { value: string }) {
  const normalized = value.toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
  return <span className={`status-badge ${normalized}`}>{value}</span>;
}

function llmConfigDraftFromConfig(config: LLMGovernanceConfig): Record<string, string> {
  const keys: Array<keyof LLMGovernanceConfig> = [
    "provider",
    "model",
    "vertex_location",
    "model_routes",
    "max_attempts",
    "retry_backoff_ms",
    "chat_timeout_ms",
    "skill_timeout_ms",
    "daily_token_quota",
    "daily_request_quota",
    "daily_cost_quota_usd",
    "input_cost_per_million",
    "output_cost_per_million",
    "failure_threshold",
    "circuit_cooldown_seconds"
  ];
  return Object.fromEntries(keys.map((key) => [key, config[key] == null ? "" : String(config[key])]));
}

function llmConfigFromDraft(draft: Record<string, string>): LLMGovernanceConfig {
  type IntegerLLMConfigKey = "max_attempts" | "retry_backoff_ms" | "chat_timeout_ms" | "skill_timeout_ms" | "daily_token_quota" | "daily_request_quota" | "failure_threshold" | "circuit_cooldown_seconds";
  type DecimalLLMConfigKey = "daily_cost_quota_usd" | "input_cost_per_million" | "output_cost_per_million";
  const integerKeys: IntegerLLMConfigKey[] = [
    "max_attempts",
    "retry_backoff_ms",
    "chat_timeout_ms",
    "skill_timeout_ms",
    "daily_token_quota",
    "daily_request_quota",
    "failure_threshold",
    "circuit_cooldown_seconds"
  ];
  const decimalKeys: DecimalLLMConfigKey[] = [
    "daily_cost_quota_usd",
    "input_cost_per_million",
    "output_cost_per_million"
  ];
  const next: LLMGovernanceConfig = {};
  const model = String(draft.model || "").trim();
  if (model) next.model = model;
  for (const key of integerKeys) {
    const raw = String(draft[key] || "").trim();
    if (!raw) continue;
    const value = Number(raw);
    if (!Number.isInteger(value)) throw new Error(`${key} must be an integer`);
    next[key] = value;
  }
  for (const key of decimalKeys) {
    const raw = String(draft[key] || "").trim();
    if (!raw) continue;
    const value = Number(raw);
    if (!Number.isFinite(value)) throw new Error(`${key} must be a number`);
    next[key] = value;
  }
  return next;
}

function modelOptionLocation(config: LLMGovernanceConfig | undefined, model: string | undefined): string {
  const selected = String(model || "").trim();
  return config?.allowed_models?.find((option) => option.id === selected)?.vertex_location || "";
}

function AdminMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="admin-metric">
      <small>{label}</small>
      <strong>{value}</strong>
    </div>
  );
}

function SkillDetailModal({
  refElement,
  skill,
  onInsert,
  onClose
}: {
  refElement: RefObject<HTMLElement | null>;
  skill: Skill;
  onInsert: (skill: Skill) => void;
  onClose: () => void;
}) {
  const title = skill.display_name || skill.name;
  const examples = skill.usage_examples || [];
  const outputTypes = skill.output_artifact_types || [];
  return (
    <div className="modal-backdrop">
      <section className="skill-modal" ref={refElement as RefObject<HTMLElement>} role="dialog" aria-modal="true" aria-labelledby="skill-modal-title" tabIndex={-1}>
        <header>
          <div className="skill-modal-heading">
            <SkillGlyph skill={skill} />
            <div>
              <h2 id="skill-modal-title">{title}</h2>
              <small>/{skill.name}</small>
            </div>
          </div>
          <button className="icon ghost" onClick={onClose} aria-label="Close skill details" title="Close">
            <X size={18} />
          </button>
        </header>
        <div className="skill-modal-body">
          <p>{skill.long_description || skill.description || skill.usage || "No description available."}</p>
          <div className="skill-detail-grid">
            <SkillFact label="Category" value={skill.category || "General"} />
            <SkillFact label="Version" value={skill.version ? `v${skill.version}` : "Unversioned"} />
            <SkillFact label="Execution" value={skill.run_as_job ? "Runs as job" : "Inline"} />
            <SkillFact label="Duration" value={skill.expected_duration || "Not specified"} />
          </div>
          {skill.tags?.length ? (
            <div className="skill-chip-row">{skill.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>
          ) : null}
          {outputTypes.length ? (
            <section className="skill-detail-section">
              <h3>Outputs</h3>
              <div className="skill-chip-row">{outputTypes.map((type) => <span key={type}>{type}</span>)}</div>
            </section>
          ) : null}
          {examples.length ? (
            <section className="skill-detail-section">
              <h3>Examples</h3>
              <ul>
                {examples.map((example) => <li key={example}>{example}</li>)}
              </ul>
            </section>
          ) : null}
        </div>
        <footer>
          <button className="primary skill-modal-insert" onClick={() => onInsert(skill)}>
            <PlayCircle size={16} />
            <span>Apply /{skill.name}</span>
          </button>
        </footer>
      </section>
    </div>
  );
}

type SkillPolicyDraft = {
  allowedTools: string;
  allowedEnv: string;
  networkAllowlist: string;
  artifactContentTypes: string;
  shellTimeout: string;
  sandboxRunner: string;
  sandboxImage: string;
  sandboxNetwork: string;
  sandboxMemory: string;
  sandboxCpus: string;
  sandboxPidsLimit: string;
  sandboxTmpfsSize: string;
  sandboxMaxOutputBytes: string;
};

const emptySkillPolicyDraft: SkillPolicyDraft = {
  allowedTools: "",
  allowedEnv: "",
  networkAllowlist: "",
  artifactContentTypes: "",
  shellTimeout: "",
  sandboxRunner: "",
  sandboxImage: "",
  sandboxNetwork: "",
  sandboxMemory: "",
  sandboxCpus: "",
  sandboxPidsLimit: "",
  sandboxTmpfsSize: "",
  sandboxMaxOutputBytes: ""
};

function SkillPolicyModal({
  api,
  skill,
  adminToken,
  onAdminTokenChange,
  onSaved,
  onClose
}: {
  api: ApiClient;
  skill: Skill;
  adminToken: string;
  onAdminTokenChange: (token: string) => void;
  onSaved: (skill: AdminSkill) => void;
  onClose: () => void;
}) {
  const modalRef = useFocusTrap<HTMLElement>(true, onClose);
  const [loadedSkill, setLoadedSkill] = useState<AdminSkill | null>(null);
  const [basePolicy, setBasePolicy] = useState<Record<string, unknown>>({});
  const [draft, setDraft] = useState<SkillPolicyDraft>(emptySkillPolicyDraft);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const updateDraft = (key: keyof SkillPolicyDraft, value: string) => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const loadPolicy = async () => {
    const token = adminToken.trim();
    if (!token) {
      setError("Admin token is required.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const adminSkills = await api.adminSkills(token);
      const record = adminSkills.find((item) => item.name === skill.name);
      if (!record) throw new Error(`/${skill.name} was not found in the admin registry.`);
      const policy = skillPolicyFromMetadata(record.metadata);
      setLoadedSkill(record);
      setBasePolicy(policy);
      setDraft(policyDraftFromConfig(policy));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    setLoadedSkill(null);
    setBasePolicy({});
    setDraft(emptySkillPolicyDraft);
    setError("");
    if (adminToken.trim()) {
      void loadPolicy();
    }
  }, [skill.name]);

  const savePolicy = async () => {
    const token = adminToken.trim();
    if (!token) {
      setError("Admin token is required.");
      return;
    }
    if (!loadedSkill) {
      setError("Load the current registry policy before saving.");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const policy = skillPolicyConfigFromDraft(basePolicy, draft);
      const updated = await api.updateAdminSkill(skill.name, token, { metadata: { policy } });
      setLoadedSkill(updated);
      const nextPolicy = skillPolicyFromMetadata(updated.metadata);
      setBasePolicy(nextPolicy);
      setDraft(policyDraftFromConfig(nextPolicy));
      onSaved(updated);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop">
      <section className="skill-policy-modal" ref={modalRef} role="dialog" aria-modal="true" aria-labelledby="skill-policy-title" tabIndex={-1}>
        <header>
          <div className="skill-modal-heading">
            <SkillGlyph skill={skill} />
            <div>
              <h2 id="skill-policy-title">Policy for /{skill.name}</h2>
              <small>{loadedSkill?.status ? `Registry status: ${loadedSkill.status}` : "Admin registry policy"}</small>
            </div>
          </div>
          <button className="icon ghost" onClick={onClose} aria-label="Close skill policy" title="Close">
            <X size={18} />
          </button>
        </header>
        <div className="skill-policy-body">
          <label className="policy-field policy-token">
            <span>Admin token</span>
            <input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
            <button type="button" className="skill-action" onClick={loadPolicy} disabled={loading}>
              {loading ? <RefreshCw size={15} /> : <ShieldCheck size={15} />}
              <span>{loading ? "Loading" : "Load"}</span>
            </button>
          </label>
          {error && <div className="policy-error"><AlertCircle size={15} /> {error}</div>}
          <section className="policy-section">
            <h3>Permissions</h3>
            <PolicyTextArea label="Allowed tools" value={draft.allowedTools} onChange={(value) => updateDraft("allowedTools", value)} placeholder={"Read\nWrite\nBash"} />
            <PolicyTextArea label="Allowed env" value={draft.allowedEnv} onChange={(value) => updateDraft("allowedEnv", value)} placeholder={"GOOGLE_APPLICATION_CREDENTIALS\nOPENAI_API_KEY"} />
            <PolicyTextArea label="Allowed domains" value={draft.networkAllowlist} onChange={(value) => updateDraft("networkAllowlist", value)} placeholder={"example.com\napi.example.com"} />
            <PolicyTextArea label="Artifact content types" value={draft.artifactContentTypes} onChange={(value) => updateDraft("artifactContentTypes", value)} placeholder={"text/markdown\napplication/vnd.openxmlformats-officedocument.wordprocessingml.document"} />
            <label className="policy-field">
              <span>Shell timeout</span>
              <input value={draft.shellTimeout} onChange={(event) => updateDraft("shellTimeout", event.currentTarget.value)} placeholder="90s, 2m" />
            </label>
          </section>
          <section className="policy-section">
            <h3>Sandbox</h3>
            <div className="policy-grid">
              <label className="policy-field">
                <span>Runner</span>
                <input value={draft.sandboxRunner} onChange={(event) => updateDraft("sandboxRunner", event.currentTarget.value)} placeholder="docker" />
              </label>
              <label className="policy-field">
                <span>Image</span>
                <input value={draft.sandboxImage} onChange={(event) => updateDraft("sandboxImage", event.currentTarget.value)} placeholder="python:3.12-slim" />
              </label>
              <label className="policy-field">
                <span>Network</span>
                <input value={draft.sandboxNetwork} onChange={(event) => updateDraft("sandboxNetwork", event.currentTarget.value)} placeholder="none, bridge" />
              </label>
              <label className="policy-field">
                <span>Memory</span>
                <input value={draft.sandboxMemory} onChange={(event) => updateDraft("sandboxMemory", event.currentTarget.value)} placeholder="512m" />
              </label>
              <label className="policy-field">
                <span>CPUs</span>
                <input value={draft.sandboxCpus} onChange={(event) => updateDraft("sandboxCpus", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="policy-field">
                <span>Pids limit</span>
                <input inputMode="numeric" value={draft.sandboxPidsLimit} onChange={(event) => updateDraft("sandboxPidsLimit", event.currentTarget.value)} placeholder="128" />
              </label>
              <label className="policy-field">
                <span>Tmpfs size</span>
                <input value={draft.sandboxTmpfsSize} onChange={(event) => updateDraft("sandboxTmpfsSize", event.currentTarget.value)} placeholder="64m" />
              </label>
              <label className="policy-field">
                <span>Max output bytes</span>
                <input inputMode="numeric" value={draft.sandboxMaxOutputBytes} onChange={(event) => updateDraft("sandboxMaxOutputBytes", event.currentTarget.value)} placeholder="1048576" />
              </label>
            </div>
          </section>
        </div>
        <footer>
          <button className="skill-action" onClick={onClose}>Cancel</button>
          <button className="primary skill-modal-insert" onClick={savePolicy} disabled={saving || loading || !loadedSkill}>
            <ShieldCheck size={16} />
            <span>{saving ? "Saving" : "Save policy"}</span>
          </button>
        </footer>
      </section>
    </div>
  );
}

function PolicyTextArea({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="policy-field">
      <span>{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.currentTarget.value)} placeholder={placeholder} rows={3} />
    </label>
  );
}

function skillPolicyFromMetadata(metadata?: Record<string, unknown>): Record<string, unknown> {
  if (!metadata) return {};
  for (const key of ["policy", "permissions", "runtime_policy", "runtimePolicy"]) {
    if (isRecord(metadata[key])) return { ...metadata[key] };
  }
  for (const key of ["agentapi", "runtime", "openclaw"]) {
    const nested = metadata[key];
    if (!isRecord(nested)) continue;
    for (const policyKey of ["policy", "permissions", "runtime_policy", "runtimePolicy"]) {
      if (isRecord(nested[policyKey])) return { ...nested[policyKey] };
    }
  }
  return {};
}

function policyDraftFromConfig(policy: Record<string, unknown>): SkillPolicyDraft {
  const sandbox = isRecord(policy.sandbox) ? policy.sandbox : {};
  return {
    allowedTools: joinPolicyList(policy.allowed_tools ?? policy.allowedTools ?? policy.tools),
    allowedEnv: joinPolicyList(policy.allowed_env ?? policy.allowedEnv ?? policy.env),
    networkAllowlist: joinPolicyList(policy.network_allowlist ?? policy.networkAllowlist ?? policy.allowed_domains ?? policy.allowedDomains ?? policy.domains),
    artifactContentTypes: joinPolicyList(policy.artifact_content_types ?? policy.artifactContentTypes ?? policy.artifact_types ?? policy.artifactTypes ?? policy.output_artifact_types ?? policy.outputArtifactTypes),
    shellTimeout: stringPolicyValue(policy.shell_timeout ?? policy.shellTimeout ?? policy.timeout),
    sandboxRunner: stringPolicyValue(sandbox.runner),
    sandboxImage: stringPolicyValue(sandbox.image),
    sandboxNetwork: stringPolicyValue(sandbox.network),
    sandboxMemory: stringPolicyValue(sandbox.memory),
    sandboxCpus: stringPolicyValue(sandbox.cpus ?? sandbox.cpu),
    sandboxPidsLimit: stringPolicyValue(sandbox.pids_limit ?? sandbox.pidsLimit),
    sandboxTmpfsSize: stringPolicyValue(sandbox.tmpfs_size ?? sandbox.tmpfsSize),
    sandboxMaxOutputBytes: stringPolicyValue(sandbox.max_output_bytes ?? sandbox.maxOutputBytes)
  };
}

function skillPolicyConfigFromDraft(base: Record<string, unknown>, draft: SkillPolicyDraft): SkillPolicyConfig {
  const next: SkillPolicyConfig = { ...base };
  setPolicyList(next, "allowed_tools", draft.allowedTools);
  setPolicyList(next, "allowed_env", draft.allowedEnv);
  setPolicyList(next, "network_allowlist", draft.networkAllowlist);
  setPolicyList(next, "artifact_content_types", draft.artifactContentTypes);
  setPolicyString(next, "shell_timeout", draft.shellTimeout);
  const sandbox: Record<string, unknown> = isRecord(next.sandbox) ? { ...next.sandbox } : {};
  setPolicyString(sandbox, "runner", draft.sandboxRunner);
  setPolicyString(sandbox, "image", draft.sandboxImage);
  setPolicyString(sandbox, "network", draft.sandboxNetwork);
  setPolicyString(sandbox, "memory", draft.sandboxMemory);
  setPolicyString(sandbox, "cpus", draft.sandboxCpus);
  setPolicyNumber(sandbox, "pids_limit", draft.sandboxPidsLimit);
  setPolicyString(sandbox, "tmpfs_size", draft.sandboxTmpfsSize);
  setPolicyNumber(sandbox, "max_output_bytes", draft.sandboxMaxOutputBytes);
  if (Object.keys(sandbox).length) next.sandbox = sandbox;
  else delete next.sandbox;
  return next;
}

function setPolicyList(target: Record<string, unknown>, key: string, value: string) {
  const list = splitPolicyList(value);
  if (list.length) target[key] = list;
  else delete target[key];
}

function setPolicyString(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (cleaned) target[key] = cleaned;
  else delete target[key];
}

function setPolicyNumber(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (!cleaned) {
    delete target[key];
    return;
  }
  const parsed = Number(cleaned);
  if (Number.isFinite(parsed) && parsed > 0) target[key] = Math.floor(parsed);
}

function splitPolicyList(value: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of value.split(/[\n,]/)) {
    const cleaned = item.trim();
    if (!cleaned || seen.has(cleaned)) continue;
    seen.add(cleaned);
    out.push(cleaned);
  }
  return out;
}

function joinPolicyList(value: unknown): string {
  if (Array.isArray(value)) return value.map((item) => stringPolicyValue(item)).filter(Boolean).join("\n");
  return stringPolicyValue(value);
}

function stringPolicyValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function SkillFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="skill-fact">
      <small>{label}</small>
      <strong>{value}</strong>
    </div>
  );
}

function SkillGlyph({ skill }: { skill: Skill }) {
  if (skill.icon) return <span className="skill-glyph text">{skill.icon}</span>;
  if (skill.produces_artifacts) return <span className="skill-glyph"><Archive size={17} /></span>;
  if (skill.run_as_job) return <span className="skill-glyph"><Clock size={17} /></span>;
  return <span className="skill-glyph"><Sparkles size={17} /></span>;
}

function rightPanelLabel(tab: RightPanelTab): string {
  if (tab === "skills") return "skills";
  if (tab === "jobs") return "jobs";
  if (tab === "attachments") return "attachments";
  return "artifacts";
}

function downsampleToPCM16(input: Float32Array, inputSampleRate: number, targetSampleRate: number): Uint8Array {
  if (!input.length || inputSampleRate <= 0 || targetSampleRate <= 0) return new Uint8Array();
  const ratio = Math.max(inputSampleRate / targetSampleRate, 1);
  const outputLength = Math.floor(input.length / ratio);
  const bytes = new Uint8Array(outputLength * 2);
  const view = new DataView(bytes.buffer);
  for (let index = 0; index < outputLength; index += 1) {
    const start = Math.floor(index * ratio);
    const end = Math.min(Math.floor((index + 1) * ratio), input.length);
    let total = 0;
    const count = Math.max(end - start, 1);
    for (let sourceIndex = start; sourceIndex < end; sourceIndex += 1) {
      total += input[sourceIndex] || 0;
    }
    const sample = Math.max(-1, Math.min(1, total / count));
    view.setInt16(index * 2, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
  }
  return bytes;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return window.btoa(binary);
}

function base64ToBytes(value: string): Uint8Array {
  const binary = window.atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function base64PCMToFloat32(value: string, mimeType: string): Float32Array {
  const bytes = base64ToBytes(value);
  const sampleCount = Math.floor(bytes.byteLength / 2);
  const samples = new Float32Array(sampleCount);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const littleEndian = !/audio\/l16/i.test(mimeType);
  for (let index = 0; index < sampleCount; index += 1) {
    samples[index] = Math.max(-1, Math.min(1, view.getInt16(index * 2, littleEndian) / 32768));
  }
  return samples;
}

function sampleRateFromMime(mime: string): number {
  const match = /(?:^|;)rate=(\d+)/i.exec(mime);
  return match ? Number.parseInt(match[1], 10) : 0;
}

function resizeComposerInput(element: HTMLTextAreaElement | null) {
  if (!element) return;
  element.style.height = "auto";
  const styles = window.getComputedStyle(element);
  const minHeight = Number.parseFloat(styles.minHeight) || 44;
  const maxHeight = Number.parseFloat(styles.maxHeight) || 180;
  const nextHeight = Math.min(Math.max(element.scrollHeight, minHeight), maxHeight);
  element.style.height = `${nextHeight}px`;
  element.style.overflowY = element.scrollHeight > maxHeight ? "auto" : "hidden";
}

function compareSkills(a: Skill, b: Skill): number {
  if (Boolean(a.featured) !== Boolean(b.featured)) return a.featured ? -1 : 1;
  const orderA = a.sort_order ?? Number.MAX_SAFE_INTEGER;
  const orderB = b.sort_order ?? Number.MAX_SAFE_INTEGER;
  if (orderA !== orderB) return orderA - orderB;
  return (a.display_name || a.name).localeCompare(b.display_name || b.name);
}

function recentSkillsFromNames(skills: Skill[], names: string[]): Skill[] {
  const byName = new Map(skills.map((skill) => [skill.name, skill]));
  const seenNames = new Set<string>();
  const out: Skill[] = [];
  for (const name of names) {
    const skill = byName.get(name);
    if (!skill || seenNames.has(skill.name)) continue;
    seenNames.add(skill.name);
    out.push(skill);
  }
  return out;
}

function readRecentSkills(): string[] {
  if (typeof window === "undefined") return [];
  try {
    const parsed = JSON.parse(window.localStorage.getItem(recentSkillsStorageKey) || "[]");
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string").slice(0, 6) : [];
  } catch {
    return [];
  }
}

function writeRecentSkills(names: string[]) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(recentSkillsStorageKey, JSON.stringify(names.slice(0, 6)));
  } catch {
    // Ignore storage failures; recent skills are only a local convenience.
  }
}

function readAdminToken(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(adminTokenStorageKey) || "";
  } catch {
    return "";
  }
}

function writeAdminToken(token: string) {
  if (typeof window === "undefined") return;
  try {
    const cleaned = token.trim();
    if (cleaned) window.localStorage.setItem(adminTokenStorageKey, cleaned);
    else window.localStorage.removeItem(adminTokenStorageKey);
  } catch {
    // Ignore storage failures; the token can be re-entered.
  }
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return "0%";
  const percent = value > 1 ? value : value * 100;
  return `${percent.toFixed(percent >= 10 ? 0 : 1)}%`;
}

function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return new Intl.NumberFormat().format(value);
}

function formatUSD(value: number): string {
  if (!Number.isFinite(value)) return "$0.00";
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: value < 1 ? 4 : 2 }).format(value);
}

function metricNumber(metrics: Record<string, unknown> | undefined, key: string): number {
  const value = metrics?.[key];
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function selectedRunPassRate(run: EvaluationRun | null): number {
  if (!run || !run.total) return 0;
  return run.passed / run.total;
}

function buildEvaluationThresholds(draft: Record<string, string>): EvaluationThresholds {
  const thresholds: EvaluationThresholds = {};
  setOptionalNumber(thresholds, "min_success_rate", draft.min_success_rate);
  setOptionalNumber(thresholds, "max_tool_error_rate", draft.max_tool_error_rate);
  setOptionalNumber(thresholds, "max_llm_error_rate", draft.max_llm_error_rate);
  setOptionalNumber(thresholds, "max_high_risk_count", draft.max_high_risk_count, true);
  setOptionalNumber(thresholds, "max_p95_latency_ms", draft.max_p95_latency_ms, true);
  setOptionalNumber(thresholds, "max_cost_usd", draft.max_cost_usd);
  return thresholds;
}

function setOptionalNumber(target: EvaluationThresholds, key: keyof EvaluationThresholds, raw: string | undefined, integer = false): void {
  const clean = String(raw || "").trim();
  if (!clean) return;
  const parsed = Number(clean);
  if (!Number.isFinite(parsed)) return;
  target[key] = integer ? Math.max(0, Math.round(parsed)) : Math.max(0, parsed);
}

function downloadTextFile(filename: string, content: string, type: string): void {
  const blob = new Blob([content], { type });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function mergeEvaluationReviews(current: EvaluationReview[], next: EvaluationReview[]): EvaluationReview[] {
  const byID = new Map<string, EvaluationReview>();
  current.forEach((review) => byID.set(review.id, review));
  next.forEach((review) => byID.set(review.id, review));
  return Array.from(byID.values()).sort((a, b) => String(b.updated_at || b.created_at).localeCompare(String(a.updated_at || a.created_at)));
}

function filterEvaluationResults(results: EvaluationResult[], filter: { status: string; userID: string; sessionID: string; jobID: string; skillName: string; provider: string; model: string; subjectType: string }): EvaluationResult[] {
  return results.filter((result) => {
    if (filter.status !== "all" && result.status !== filter.status) return false;
    if (filter.subjectType !== "all" && result.subject_type !== filter.subjectType) return false;
    if (filter.userID && result.user_id !== filter.userID) return false;
    if (filter.sessionID && result.session_id !== filter.sessionID) return false;
    if (filter.jobID && result.job_id !== filter.jobID) return false;
    if (filter.skillName && result.skill_name !== filter.skillName) return false;
    if (filter.provider && result.provider !== filter.provider) return false;
    if (filter.model && result.model !== filter.model) return false;
    return true;
  });
}

function auditRecordSummary(record: AuditLogRecord): string {
  const target = record.session_id || record.job_id || record.asset_id || record.request_id || record.id;
  return `${record.user_id || "system"} · ${formatTime(record.created_at)} · ${target}`;
}

function riskEventSummary(event: RiskEvent): string {
  const actor = event.user_id || event.ip_address || "anonymous";
  return `${actor} · ${formatTime(event.created_at)} · +${event.score_delta}`;
}

function formatAuditMetadata(record: AuditLogRecord): string {
  const payload = {
    metadata: record.metadata || {},
    user_agent: record.user_agent || "",
    request_id: record.request_id || ""
  };
  return JSON.stringify(payload, null, 2);
}

function auditRiskForEventName(event: string): string {
  const normalized = event.toLowerCase();
  if (["account_delete", "memory_delete_user", "data_export", "user_ban", "user_disable", "skill_disable", "skill_policy_update", "admin_job_cancel"].includes(normalized)) return "high";
  if (normalized.includes("delete") || normalized.includes("disable") || normalized.includes("ban") || normalized.includes("policy")) return "high";
  if (normalized.includes("cancel") || normalized.includes("publish") || normalized.includes("unpublish") || normalized.includes("update") || normalized.includes("memory_")) return "medium";
  return "low";
}

function formatShortDate(value?: string): string {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return date.toLocaleDateString();
}

function initials(value: string): string {
  const parts = value.trim().split(/[\s@._-]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0]?.toUpperCase()).join("");
  return letters || "U";
}

function fuzzyMatch(query: string, fields: Array<string | number | undefined | null>): boolean {
  const normalizedQuery = normalizeSearch(query);
  if (!normalizedQuery) return true;
  const rawHaystack = fields.filter((field) => field !== undefined && field !== null).join(" ");
  const haystack = normalizeSearch(rawHaystack);
  if (!haystack) return false;
  if (haystack.includes(normalizedQuery)) return true;
  return acronymSearch(rawHaystack).includes(normalizedQuery);
}

function normalizeSearch(value: string | number | undefined | null): string {
  return String(value || "").toLowerCase().replace(/[\s_\-./]+/g, "");
}

function acronymSearch(value: string): string {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/i)
    .filter(Boolean)
    .map((word) => word[0])
    .join("");
}

function useFocusTrap<T extends HTMLElement>(active: boolean, onEscape: () => void) {
  const containerRef = useRef<T | null>(null);
  const onEscapeRef = useRef(onEscape);

  useEffect(() => {
    onEscapeRef.current = onEscape;
  }, [onEscape]);

  useEffect(() => {
    if (!active) return;
    const container = containerRef.current;
    if (!container) return;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focusFirst = () => {
      const target = focusableElements(container)[0] || container;
      target.focus();
    };
    const frame = window.requestAnimationFrame(focusFirst);
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onEscapeRef.current();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = focusableElements(container);
      if (!focusable.length) {
        event.preventDefault();
        container.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const current = document.activeElement;
      if (event.shiftKey && current === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && current === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", handleKeyDown);
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [active]);

  return containerRef;
}

function focusableElements(container: HTMLElement): HTMLElement[] {
  const selector = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled]):not([type='hidden'])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",");
  return Array.from(container.querySelectorAll<HTMLElement>(selector))
    .filter((element) => !element.closest("[aria-hidden='true']") && element.getClientRects().length > 0);
}

function Progress({ value }: { value: number }) {
  return <div className="progress"><span style={{ width: `${Math.max(0, Math.min(100, value))}%` }} /></div>;
}

function AssetList({
  assets,
  icon,
  emptyLabel = "No items",
  preview,
  download,
  remove,
  extractMemory,
  memoryBusy = {},
  memoryDisabled = false,
  addToMessage
}: {
  assets: Asset[];
  icon: "file" | "image";
  emptyLabel?: string;
  preview: (asset: Asset) => void;
  download: (id: string) => void;
  remove: (id: string) => void;
  extractMemory?: (asset: Asset) => void;
  memoryBusy?: Record<string, boolean>;
  memoryDisabled?: boolean;
  addToMessage?: (asset: Asset) => void;
}) {
  if (!assets.length) return <div className="empty-small">{emptyLabel}</div>;
  const Icon = icon === "image" ? Image : FileUp;
  return (
    <div className="asset-list">
      {assets.map((asset) => (
        <div key={asset.id} className={`asset-row ${addToMessage ? "with-add" : ""} ${extractMemory ? "with-memory" : ""}`}>
          <Icon size={18} />
          <div>
            <strong>{asset.filename}</strong>
            <small>{formatBytes(asset.size_bytes)} · {formatTime(asset.created_at)}</small>
          </div>
          {addToMessage && (
            <button className="icon" onClick={() => addToMessage(asset)} title="Add to message" aria-label={`Add ${asset.filename} to message`}>
              <MessageSquarePlus size={16} />
            </button>
          )}
          {extractMemory && (
            <button
              className="icon"
              onClick={() => extractMemory(asset)}
              disabled={memoryDisabled || Boolean(memoryBusy[asset.id])}
              title={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${asset.filename}`}
              aria-label={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${asset.filename}`}
            >
              <Brain size={16} />
            </button>
          )}
          <button className="icon" onClick={() => preview(asset)} title={`Preview ${asset.filename}`} aria-label={`Preview ${asset.filename}`}>
            <AssetPreviewIcon asset={asset} />
          </button>
          <button className="icon" onClick={() => download(asset.id)} title={`Download ${asset.filename}`} aria-label={`Download ${asset.filename}`}><Download size={16} /></button>
          <button className="icon danger" onClick={() => remove(asset.id)} title={`Delete ${asset.filename}`} aria-label={`Delete ${asset.filename}`}><Trash2 size={16} /></button>
        </div>
      ))}
    </div>
  );
}

function AssetPreviewIcon({ asset }: { asset: Asset }) {
  if (isTextAsset(asset) || isPDFAsset(asset)) return <FileText size={16} />;
  return <Image size={16} />;
}

const personalizationStyleOptions = [
  { value: "default", label: "Default" },
  { value: "professional_reliable", label: "Professional reliable" },
  { value: "friendly", label: "Friendly" },
  { value: "direct", label: "Direct" },
  { value: "imaginative", label: "Imaginative" },
  { value: "efficient", label: "Efficient" },
  { value: "witty", label: "Witty" }
];

const personalizationTraitOptions = [
  { value: "enhanced", label: "Enhanced" },
  { value: "default", label: "Default" },
  { value: "reduced", label: "Reduced" }
];
const personalizationTextLimits = {
  nickname: 120,
  occupation: 160,
  about: 2000,
  customInstructions: 4000
};

function normalizePersonalizationDraft(settings: PersonalizationSettings): PersonalizationSettings {
  return {
    ...settings,
    profile: {
      nickname: settings.profile.nickname || "",
      occupation: settings.profile.occupation || "",
      about: settings.profile.about || ""
    },
    custom_instructions: settings.custom_instructions || ""
  };
}

function personalizationPatchFromDraft(settings: PersonalizationSettings): Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">> {
  return {
    profile: settings.profile,
    style: settings.style,
    traits: settings.traits,
    custom_instructions: settings.custom_instructions,
    feature_flags: settings.feature_flags
  };
}

function SettingsModal({
  userLabel,
  memorySettings,
  personalizationSettings,
  personalizationSaving,
  hasSession,
  onUpdateMemorySettings,
  onUpdatePersonalization,
  onResetPersonalization,
  onManageMemory,
  onDeleteSessionMemory,
  onDeleteAllMemory,
  onExportData,
  onDeleteAccount,
  onLogout,
  onClose
}: {
  userLabel: string;
  memorySettings: MemorySettings;
  personalizationSettings: PersonalizationSettings;
  personalizationSaving: boolean;
  hasSession: boolean;
  onUpdateMemorySettings: (patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) => void;
  onUpdatePersonalization: (patch: Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">>) => void;
  onResetPersonalization: () => void;
  onManageMemory: () => void;
  onDeleteSessionMemory: () => void;
  onDeleteAllMemory: () => void;
  onExportData: () => void;
  onDeleteAccount: () => void;
  onLogout: () => void;
  onClose: () => void;
}) {
  const modalRef = useFocusTrap<HTMLElement>(true, onClose);
  const [activeSection, setActiveSection] = useState<"personalization" | "data" | "account">("personalization");
  const [draftPersonalization, setDraftPersonalization] = useState<PersonalizationSettings>(personalizationSettings);

  useEffect(() => {
    setDraftPersonalization(personalizationSettings);
  }, [personalizationSettings]);

  const personalizationDirty = JSON.stringify(normalizePersonalizationDraft(draftPersonalization)) !== JSON.stringify(normalizePersonalizationDraft(personalizationSettings));

  return (
    <div className="modal-backdrop settings-backdrop" onClick={onClose}>
      <section
        className="settings-modal"
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
      >
        <aside className="settings-nav" aria-label="Settings sections">
          <button className="icon settings-close" onClick={onClose} title="Close settings" aria-label="Close settings">
            <X size={22} />
          </button>
          <button className={`settings-nav-item ${activeSection === "personalization" ? "active" : ""}`} onClick={() => setActiveSection("personalization")}><Brain size={18} /> Personalization</button>
          <button className={`settings-nav-item ${activeSection === "data" ? "active" : ""}`} onClick={() => setActiveSection("data")}><Database size={18} /> Data controls</button>
          <button className={`settings-nav-item ${activeSection === "account" ? "active" : ""}`} onClick={() => setActiveSection("account")}><UserX size={18} /> Account</button>
        </aside>
        <section className="settings-panel">
          {activeSection === "personalization" && (
            <PersonalizationSettingsPanel
              userLabel={userLabel}
              draft={draftPersonalization}
              dirty={personalizationDirty}
              saving={personalizationSaving}
              memorySettings={memorySettings}
              onDraftChange={setDraftPersonalization}
              onSave={() => onUpdatePersonalization(personalizationPatchFromDraft(draftPersonalization))}
              onReset={onResetPersonalization}
              onManageMemory={onManageMemory}
            />
          )}
          {activeSection === "data" && (
            <DataControlsSettingsPanel
              userLabel={userLabel}
              memorySettings={memorySettings}
              hasSession={hasSession}
              onUpdateMemorySettings={onUpdateMemorySettings}
              onManageMemory={onManageMemory}
              onDeleteSessionMemory={onDeleteSessionMemory}
              onDeleteAllMemory={onDeleteAllMemory}
              onExportData={onExportData}
            />
          )}
          {activeSection === "account" && (
            <AccountSettingsPanel
              userLabel={userLabel}
              onLogout={onLogout}
              onDeleteAccount={onDeleteAccount}
            />
          )}
        </section>
      </section>
    </div>
  );
}

function PersonalizationSettingsPanel({
  userLabel,
  draft,
  dirty,
  saving,
  memorySettings,
  onDraftChange,
  onSave,
  onReset,
  onManageMemory
}: {
  userLabel: string;
  draft: PersonalizationSettings;
  dirty: boolean;
  saving: boolean;
  memorySettings: MemorySettings;
  onDraftChange: (settings: PersonalizationSettings) => void;
  onSave: () => void;
  onReset: () => void;
  onManageMemory: () => void;
}) {
  const updateProfile = (patch: Partial<PersonalizationSettings["profile"]>) => {
    onDraftChange({ ...draft, profile: { ...draft.profile, ...patch } });
  };
  const updateStyle = (patch: Partial<PersonalizationSettings["style"]>) => {
    onDraftChange({ ...draft, style: { ...draft.style, ...patch } });
  };
  const updateTraits = (patch: Partial<PersonalizationSettings["traits"]>) => {
    onDraftChange({ ...draft, traits: { ...draft.traits, ...patch } });
  };
  const updateFlags = (patch: Partial<PersonalizationSettings["feature_flags"]>) => {
    onDraftChange({ ...draft, feature_flags: { ...draft.feature_flags, ...patch } });
  };
  const saveState = saving ? "Saving changes..." : dirty ? "Unsaved changes" : "All changes saved";

  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Personalization</h2>
          <small>{userLabel}</small>
        </div>
      </header>

      <div className="settings-section-title">
        <strong>Response behavior</strong>
        <p>Controls that are applied before saved memory and recent chat history.</p>
      </div>
      <div className="settings-row">
        <div>
          <strong>Basic style and tone</strong>
          <p>Choose the response style used before memory or conversation context is considered.</p>
        </div>
        <select className="settings-select" value={draft.style.preset} onChange={(event) => updateStyle({ preset: event.target.value })} aria-label="Basic style">
          {personalizationStyleOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </div>
      <div className="settings-row">
        <div>
          <strong>Tone</strong>
          <p>Fine-tune the overall speaking tone independently from the base style.</p>
        </div>
        <select className="settings-select" value={draft.style.tone} onChange={(event) => updateStyle({ tone: event.target.value })} aria-label="Tone">
          {personalizationStyleOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </div>
      <div className="settings-fieldset">
        <div>
          <strong>Traits</strong>
          <p>Adjust optional behavior layered on top of the selected style.</p>
        </div>
        <div className="settings-trait-grid">
          <LabeledSelect label="Warmth" value={draft.traits.warmth} options={personalizationTraitOptions} onChange={(value) => updateTraits({ warmth: value })} />
          <LabeledSelect label="Enthusiasm" value={draft.traits.enthusiasm} options={personalizationTraitOptions} onChange={(value) => updateTraits({ enthusiasm: value })} />
          <LabeledSelect label="Headings and lists" value={draft.traits.headings_and_lists} options={personalizationTraitOptions} onChange={(value) => updateTraits({ headings_and_lists: value })} />
          <LabeledSelect label="Emoji" value={draft.traits.emoji} options={personalizationTraitOptions} onChange={(value) => updateTraits({ emoji: value })} />
        </div>
      </div>
      <div className="settings-row">
        <div>
          <strong>Quick answers</strong>
          <p>Simple questions get direct, compact answers; complex work still uses normal depth.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.quick_answers} label="Quick answers" onChange={(checked) => updateFlags({ quick_answers: checked })} />
      </div>
      <div className="settings-section-title">
        <strong>Instructions</strong>
        <p>Explicit instructions override saved memory when they conflict.</p>
      </div>
      <div className="settings-fieldset">
        <div className="settings-label-row">
          <label className="settings-input-label" htmlFor="custom-instructions">Custom instructions</label>
          <CharCounter value={draft.custom_instructions} max={personalizationTextLimits.customInstructions} />
        </div>
        <textarea
          id="custom-instructions"
          className="settings-textarea"
          maxLength={personalizationTextLimits.customInstructions}
          value={draft.custom_instructions}
          onChange={(event) => onDraftChange({ ...draft, custom_instructions: event.target.value })}
          placeholder="Example: Reply in Chinese unless I ask otherwise."
        />
      </div>
      <div className="settings-section-title">
        <strong>About you</strong>
        <p>Stable profile details that should be available across conversations.</p>
      </div>
      <div className="settings-fieldset">
        <div className="settings-input-grid">
          <label className="settings-input-label">
            <span className="settings-label-row"><span>Nickname</span><CharCounter value={draft.profile.nickname || ""} max={personalizationTextLimits.nickname} /></span>
            <input className="settings-input" maxLength={personalizationTextLimits.nickname} value={draft.profile.nickname || ""} onChange={(event) => updateProfile({ nickname: event.target.value })} placeholder="What should Agent call you?" />
          </label>
          <label className="settings-input-label">
            <span className="settings-label-row"><span>Occupation</span><CharCounter value={draft.profile.occupation || ""} max={personalizationTextLimits.occupation} /></span>
            <input className="settings-input" maxLength={personalizationTextLimits.occupation} value={draft.profile.occupation || ""} onChange={(event) => updateProfile({ occupation: event.target.value })} placeholder="Product manager, engineer..." />
          </label>
        </div>
        <div className="settings-label-row">
          <label className="settings-input-label" htmlFor="about-you">Details</label>
          <CharCounter value={draft.profile.about || ""} max={personalizationTextLimits.about} />
        </div>
        <textarea
          id="about-you"
          className="settings-textarea compact"
          maxLength={personalizationTextLimits.about}
          value={draft.profile.about || ""}
          onChange={(event) => updateProfile({ about: event.target.value })}
          placeholder="Background, preferred depth, domain context, or recurring constraints."
        />
      </div>
      <div className="settings-section-title">
        <strong>Context sources</strong>
        <p>Choose which stored or external context may be referenced when answering.</p>
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference saved memory</strong>
          <p>{memorySettings.context_enabled ? "Use curated saved memory when building replies." : "Memory context is disabled in Data controls."}</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_saved_memory} label="Reference saved memory" onChange={(checked) => updateFlags({ use_saved_memory: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference recent chats</strong>
          <p>Include recent visible conversation history in model context.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_chat_history} label="Reference recent chats" onChange={(checked) => updateFlags({ use_chat_history: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference browser memory</strong>
          <p>Use browser or external context submitted through the browser memory API.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_browser_memory} label="Reference browser memory" onChange={(checked) => updateFlags({ use_browser_memory: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Saved memory</strong>
          <p>Review what the automatic memory system currently stores.</p>
        </div>
        <button className="settings-action" onClick={onManageMemory}>Manage</button>
      </div>
      <div className="settings-footer-actions">
        <span className={`settings-save-state ${dirty ? "dirty" : ""}`}>{saveState}</span>
        <button className="settings-action" onClick={onReset} disabled={saving}>Reset</button>
        <button className="settings-action primary" onClick={onSave} disabled={!dirty || saving}>{saving ? "Saving" : "Save"}</button>
      </div>
    </>
  );
}

function CharCounter({ value, max }: { value: string; max: number }) {
  const length = Array.from(value || "").length;
  return <span className="settings-char-count">{length}/{max}</span>;
}

function DataControlsSettingsPanel({
  userLabel,
  memorySettings,
  hasSession,
  onUpdateMemorySettings,
  onManageMemory,
  onDeleteSessionMemory,
  onDeleteAllMemory,
  onExportData
}: {
  userLabel: string;
  memorySettings: MemorySettings;
  hasSession: boolean;
  onUpdateMemorySettings: (patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) => void;
  onManageMemory: () => void;
  onDeleteSessionMemory: () => void;
  onDeleteAllMemory: () => void;
  onExportData: () => void;
}) {
  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Data controls</h2>
          <small>{userLabel}</small>
        </div>
      </header>

      <div className="settings-row">
        <div>
          <strong>Memory</strong>
          <p>Let AgentAPI save useful preferences and project context. Sensitive values are redacted before saving.</p>
        </div>
        <SwitchButton checked={memorySettings.enabled} label="Enable memory" onChange={(checked) => onUpdateMemorySettings({ enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Save new memory</strong>
          <p>Allow new chats, attachments, and artifacts to create memory.</p>
        </div>
        <SwitchButton checked={memorySettings.capture_enabled} label="Save new memory" onChange={(checked) => onUpdateMemorySettings({ capture_enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Use saved memory</strong>
          <p>Allow saved memory to be included in future model context.</p>
        </div>
        <SwitchButton checked={memorySettings.context_enabled} label="Use saved memory" onChange={(checked) => onUpdateMemorySettings({ context_enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Saved memory</strong>
          <p>Review, edit, delete, and resolve saved memory items.</p>
        </div>
        <button className="settings-action" onClick={onManageMemory}>Manage</button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Current session memory</strong>
          <p>Remove memory saved from the current session only.</p>
        </div>
        <button className="settings-action" onClick={onDeleteSessionMemory} disabled={!hasSession}>Delete</button>
      </div>
      <div className="settings-row">
        <div>
          <strong>All memory</strong>
          <p>Remove all saved memory for this account.</p>
        </div>
        <button className="settings-action danger-outline" onClick={onDeleteAllMemory}>Delete all</button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Export data</strong>
          <p>Download account data, sessions, artifacts, jobs, and memory.</p>
        </div>
        <button className="settings-action" onClick={onExportData}>Export</button>
      </div>
    </>
  );
}

function AccountSettingsPanel({ userLabel, onLogout, onDeleteAccount }: { userLabel: string; onLogout: () => void; onDeleteAccount: () => void }) {
  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Account</h2>
          <small>{userLabel}</small>
        </div>
      </header>
      <div className="settings-row">
        <div>
          <strong>Session</strong>
          <p>Sign out of this browser.</p>
        </div>
        <button className="settings-action" onClick={onLogout}>Log out</button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Delete account</strong>
          <p>Permanently remove this account and associated data.</p>
        </div>
        <button className="settings-action danger-outline" onClick={onDeleteAccount}>Delete</button>
      </div>
    </>
  );
}

function LabeledSelect({ label, value, options, onChange }: { label: string; value: string; options: Array<{ value: string; label: string }>; onChange: (value: string) => void }) {
  return (
    <label className="settings-input-label">
      {label}
      <select className="settings-select" value={value} onChange={(event) => onChange(event.target.value)} aria-label={label}>
        {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    </label>
  );
}

function SwitchButton({ checked, label, onChange }: { checked: boolean; label: string; onChange: (checked: boolean) => void }) {
  return (
    <button
      type="button"
      className={`switch-button ${checked ? "checked" : ""}`}
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
    >
      <span>{checked ? "On" : "Off"}</span>
    </button>
  );
}

function PreviewModal({ asset, url, onClose }: { asset: Asset; url: string; onClose: () => void }) {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  const isImage = isImageAsset(asset);
  const isPDF = isPDFAsset(asset);
  const isText = isTextAsset(asset);
  const isOffice = ["ppt", "pptx", "doc", "docx", "xls", "xlsx"].includes(ext);
  const modalRef = useFocusTrap<HTMLElement>(true, onClose);
  const [textPreview, setTextPreview] = useState<{ status: "idle" | "loading" | "loaded" | "error"; content: string; error?: string }>({
    status: "idle",
    content: ""
  });

  useEffect(() => {
    if (!isText) return;
    let cancelled = false;
    setTextPreview({ status: "loading", content: "" });
    fetch(url, { credentials: "include", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error(`Preview failed (${response.status})`);
        return response.text();
      })
      .then((content) => {
        if (!cancelled) setTextPreview({ status: "loaded", content });
      })
      .catch((error) => {
        if (!cancelled) setTextPreview({ status: "error", content: "", error: errorMessage(error) });
      });
    return () => {
      cancelled = true;
    };
  }, [isText, url]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section
        className="preview-modal"
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="preview-title"
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <strong id="preview-title">{asset.filename}</strong>
            <small>{asset.content_type || "file"} · {formatBytes(asset.size_bytes)}</small>
          </div>
          <div className="preview-actions">
            <button className="preview-download" onClick={() => window.open(url, "_blank")} title={`Download ${asset.filename}`} aria-label={`Download ${asset.filename}`}>
              <Download size={16} />
              <span>Download</span>
            </button>
            <button className="icon ghost" onClick={onClose} title="Close preview" aria-label="Close preview"><X size={18} /></button>
          </div>
        </header>
        <div className="preview-body">
          {isImage && <img src={url} alt={asset.filename} />}
          {isPDF && <iframe src={url} title={asset.filename} />}
          {isText && (
            <div className="text-preview" role="document" aria-label={asset.filename}>
              {textPreview.status === "loading" && <div className="preview-fallback">Loading preview...</div>}
              {textPreview.status === "error" && <div className="preview-fallback">{textPreview.error || "Preview failed"}</div>}
              {textPreview.status === "loaded" && <pre>{textPreview.content}</pre>}
            </div>
          )}
          {isOffice && (
            <div className="preview-fallback">
              <FileUp size={32} />
              <strong>{asset.filename}</strong>
              <p>Office previews depend on the browser or deployment viewer. Use download/open for this file.</p>
            </div>
          )}
          {!isImage && !isPDF && !isText && !isOffice && (
            <div className="preview-fallback">
              <FileUp size={32} />
              <strong>{asset.filename}</strong>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function isImageAsset(asset: Asset): boolean {
  return (asset.content_type || "").startsWith("image/");
}

function isPDFAsset(asset: Asset): boolean {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return asset.content_type === "application/pdf" || ext === "pdf";
}

function isTextAsset(asset: Asset): boolean {
  const contentType = asset.content_type || "";
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return (
    contentType.startsWith("text/") ||
    [
      "txt",
      "md",
      "markdown",
      "csv",
      "tsv",
      "json",
      "jsonl",
      "log",
      "yaml",
      "yml",
      "xml",
      "html",
      "css",
      "js",
      "jsx",
      "ts",
      "tsx",
      "go",
      "py",
      "java",
      "c",
      "cpp",
      "h",
      "sh",
      "sql",
      "toml",
      "ini",
      "env"
    ].includes(ext)
  );
}

function MemoryModal({
  items,
  actions,
  loading,
  error,
  scope,
  statusFilter,
  levelFilter,
  hasSession,
  onScopeChange,
  onStatusChange,
  onLevelChange,
  onRefresh,
  onRebuild,
  onScore,
  onRunMaintenance,
  onUpdate,
  onFeedback,
  onResolve,
  onDelete,
  onClose
}: {
  items: MemoryItem[];
  actions: MemoryMaintenanceAction[];
  loading: boolean;
  error: string;
  scope: "all" | "session";
  statusFilter: string;
  levelFilter: string;
  hasSession: boolean;
  onScopeChange: (scope: "all" | "session") => void;
  onStatusChange: (status: string) => void;
  onLevelChange: (level: string) => void;
  onRefresh: () => void;
  onRebuild: () => void;
  onScore: () => void;
  onRunMaintenance: () => void;
  onUpdate: (item: MemoryItem, patch: Partial<Pick<MemoryItem, "content" | "namespace" | "category" | "tags" | "visibility">>) => void;
  onFeedback: (item: MemoryItem, type: "important" | "incorrect" | "not_relevant") => void;
  onResolve: (item: MemoryItem, action: "accept" | "reject" | "keep_both") => void;
  onDelete: (item: MemoryItem) => void;
  onClose: () => void;
}) {
  const modalRef = useFocusTrap<HTMLElement>(true, onClose);
  const [editingId, setEditingId] = useState("");
  const [draftContent, setDraftContent] = useState("");
  const [draftNamespace, setDraftNamespace] = useState("default");
  const [draftCategory, setDraftCategory] = useState("fact");
  const [draftVisibility, setDraftVisibility] = useState("user");
  const [draftTags, setDraftTags] = useState("");

  function startEdit(item: MemoryItem) {
    setEditingId(item.id);
    setDraftContent(item.content);
    setDraftNamespace(item.namespace || "default");
    setDraftCategory(item.category || "fact");
    setDraftVisibility(item.visibility || "user");
    setDraftTags((item.tags || []).join(", "));
  }

  function cancelEdit() {
    setEditingId("");
    setDraftContent("");
    setDraftNamespace("default");
    setDraftCategory("fact");
    setDraftVisibility("user");
    setDraftTags("");
  }

  function saveEdit(item: MemoryItem) {
    onUpdate(item, {
      content: draftContent,
      namespace: draftNamespace,
      category: draftCategory,
      visibility: draftVisibility,
      tags: draftTags.split(",").map((tag) => tag.trim()).filter(Boolean)
    });
    cancelEdit();
  }

  const pendingActions = actions.filter((action) => action.status !== "dismissed");
  const conflictReviews = pendingActions.filter((action) => action.type === "confirm_conflict").length;
  const safeReviews = Math.max(0, pendingActions.length - conflictReviews);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section
        className="memory-modal"
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="memory-title"
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <strong id="memory-title">Memory</strong>
            <small>{items.length} saved item{items.length === 1 ? "" : "s"}</small>
          </div>
          <div className="memory-actions">
            <button className="icon ghost" onClick={onScore} title="Score memory quality" aria-label="Score memory quality">
              <Activity size={16} />
            </button>
            <button className="icon ghost" onClick={onRunMaintenance} title="Organize memory automatically" aria-label="Organize memory automatically">
              <Briefcase size={16} />
            </button>
            <button className="icon ghost" onClick={onRebuild} title="Rebuild memory summaries" aria-label="Rebuild memory summaries">
              <Sparkles size={16} />
            </button>
            <button className="icon ghost" onClick={onRefresh} title="Refresh memory" aria-label="Refresh memory">
              <RefreshCw size={16} />
            </button>
            <button className="icon ghost" onClick={onClose} title="Close memory" aria-label="Close memory">
              <X size={16} />
            </button>
          </div>
        </header>
        <div className="memory-scope" role="tablist" aria-label="Memory scope">
          <button type="button" className={scope === "all" ? "active" : ""} onClick={() => onScopeChange("all")}>
            All
          </button>
          <button type="button" className={scope === "session" ? "active" : ""} onClick={() => onScopeChange("session")} disabled={!hasSession}>
            Current Session
          </button>
          <select value={statusFilter} onChange={(event) => onStatusChange(event.target.value)} aria-label="Memory status filter">
            <option value="active">Active</option>
            <option value="pending_confirm">Pending</option>
            <option value="archived">Archived</option>
            <option value="conflicted">Conflicted</option>
            <option value="all">All status</option>
          </select>
          <select value={levelFilter} onChange={(event) => onLevelChange(event.target.value)} aria-label="Memory level filter">
            <option value="all">All levels</option>
            <option value="atomic">Atomic</option>
            <option value="concept">Concept</option>
            <option value="profile">Profile</option>
          </select>
        </div>
        <div className="memory-scroll-area">
          <div className="memory-system-status" aria-live="polite">
            <div>
              <strong>Automatic memory care</strong>
              <p>System-managed cleanup is active. Low-confidence changes stay visible for audit instead of being applied silently.</p>
            </div>
            <div className="memory-system-badges">
              <span>{pendingActions.length ? `${pendingActions.length} review` : "No review needed"}</span>
              {!!conflictReviews && <span>{conflictReviews} conflict{conflictReviews === 1 ? "" : "s"}</span>}
              {!!safeReviews && <span>{safeReviews} guarded</span>}
            </div>
          </div>
          <div className="memory-list">
            {loading && <div className="memory-empty">Loading memory...</div>}
            {!loading && error && <div className="memory-empty error-text">{error}</div>}
            {!loading && !error && !items.length && <div className="memory-empty">No saved memory</div>}
            {!loading && !error && items.map((item) => (
              <article key={item.id} className="memory-item">
              {editingId === item.id ? (
                <div className="memory-editor">
                  <textarea value={draftContent} onChange={(event) => setDraftContent(event.target.value)} aria-label="Memory content" />
                  <div className="memory-editor-row">
                    <input value={draftNamespace} onChange={(event) => setDraftNamespace(event.target.value)} placeholder="namespace" aria-label="Memory namespace" />
                    <select value={draftCategory} onChange={(event) => setDraftCategory(event.target.value)} aria-label="Memory category">
                      <option value="fact">Fact</option>
                      <option value="preference">Preference</option>
                      <option value="event">Event</option>
                      <option value="skill">Skill</option>
                    </select>
                    <select value={draftVisibility} onChange={(event) => setDraftVisibility(event.target.value)} aria-label="Memory visibility">
                      <option value="user">User</option>
                      <option value="private">Private</option>
                      <option value="session_only">Session only</option>
                      <option value="shared">Shared</option>
                    </select>
                    <input value={draftTags} onChange={(event) => setDraftTags(event.target.value)} placeholder="tags, comma separated" aria-label="Memory tags" />
                  </div>
                  <div className="memory-editor-actions">
                    <button type="button" onClick={cancelEdit}>Cancel</button>
                    <button type="button" className="primary" onClick={() => saveEdit(item)}>Save</button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="memory-item-meta">
                    <span>{item.level || "atomic"}</span>
                    <span>{item.status}</span>
                    <span>{item.category || item.kind}</span>
                    <span>{item.source}</span>
                    {item.namespace && <span>{item.namespace}</span>}
                    {item.visibility && <span>{item.visibility}</span>}
                    <span>{Math.round((item.confidence || 0) * 100)}% confidence</span>
                    <span>{Math.round((item.weight || 0) * 100)} weight</span>
                    {typeof item.metadata?.quality_score === "number" && <span>{Math.round(item.metadata.quality_score * 100)} quality</span>}
                    <time>{formatTime(item.updated_at || item.created_at)}</time>
                  </div>
                  <p>{item.content}</p>
                  {!!item.tags?.length && <small>{item.tags.join(", ")}</small>}
                  {item.session_id && <small>Session {item.session_id}</small>}
                  {!!item.source_refs?.length && (
                    <small>
                      Source: {item.source_refs.map((ref) => ref.filename || `${ref.kind}:${ref.id}`).join(", ")}
                    </small>
                  )}
                  {!!item.conflict_ids?.length && <small>Conflicts: {item.conflict_ids.join(", ")}</small>}
                  {(item.status === "pending_confirm" || item.status === "conflicted") && (
                    <div className="memory-resolution-actions" aria-label="Resolve memory conflict">
                      <button type="button" onClick={() => onResolve(item, "accept")}>Accept</button>
                      <button type="button" onClick={() => onResolve(item, "keep_both")}>Keep both</button>
                      <button type="button" className="danger" onClick={() => onResolve(item, "reject")}>Reject</button>
                    </div>
                  )}
                  <div className="memory-feedback-actions" aria-label="Memory feedback">
                    <button className="icon ghost" onClick={() => onFeedback(item, "important")} title="Mark memory as important" aria-label="Mark memory as important">
                      <Star size={15} />
                    </button>
                    <button className="icon ghost" onClick={() => onFeedback(item, "not_relevant")} title="Mark memory as less relevant" aria-label="Mark memory as less relevant">
                      <Archive size={15} />
                    </button>
                    <button className="icon ghost danger" onClick={() => onFeedback(item, "incorrect")} title="Mark memory as incorrect" aria-label="Mark memory as incorrect">
                      <X size={15} />
                    </button>
                  </div>
                  <div className="memory-item-actions">
                    <button className="icon ghost" onClick={() => startEdit(item)} title="Edit memory item" aria-label="Edit memory item">
                      <FileText size={16} />
                    </button>
                    <button className="icon ghost danger" onClick={() => onDelete(item)} title="Delete memory item" aria-label="Delete memory item">
                      <Trash2 size={16} />
                    </button>
                  </div>
                </>
              )}
              </article>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}

function ConfirmModal({
  dialog,
  onCancel,
  onConfirm
}: {
  dialog: ConfirmDialog;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const modalRef = useFocusTrap<HTMLElement>(true, onCancel);

  return (
    <div className="modal-backdrop confirm-backdrop" onClick={onCancel}>
      <section
        className="confirm-modal"
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        aria-describedby={dialog.detail ? "confirm-message confirm-detail" : "confirm-message"}
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
      >
        <h2 id="confirm-title">{dialog.title}</h2>
        <p id="confirm-message">{dialog.message}</p>
        {dialog.detail && <small id="confirm-detail">{dialog.detail}</small>}
        <footer>
          <button type="button" onClick={onCancel}>{dialog.cancelLabel || "Cancel"}</button>
          <button type="button" className={dialog.danger ? "danger-action" : "primary"} onClick={onConfirm}>
            {dialog.confirmLabel || "OK"}
          </button>
        </footer>
      </section>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (!bytes) return "0 KB";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatTime(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}
