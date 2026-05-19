import { FormEvent, forwardRef, lazy, ReactNode, RefObject, Suspense, useEffect, useMemo, useRef, useState } from "react";
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
import type { Asset, AuthSession, Job, JobEvent, MemoryItem, MemoryMaintenanceAction, MemorySettings, Message, MessageSearchResult, PersonalizationSettings, ReadinessStatus, RuntimeEvent, Session, Skill } from "./types";
import { readSSEStream } from "./lib/sse";
import { sessionTitle } from "./lib/sessionTitle";

const AdminConsole = lazy(() => import("./admin/AdminConsole"));

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
  const [previewAsset, setPreviewAsset] = useState<{ asset: Asset; url: string; previewUrl?: string } | null>(null);
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
  const selectedSessionIdRef = useRef("");
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
    selectedSessionIdRef.current = sessionId;
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
    if (selectedSessionIdRef.current === id) {
      setStatus({ tone: "busy", text: "Refreshing" });
    }
    const previousArtifactIds = new Set(artifactsRef.current.map((asset) => asset.id));
    const [session, jobList, attachmentList, artifactList] = await Promise.all([
      api.getSession(id),
      api.jobs(id),
      api.attachments(id),
      api.artifacts(id)
    ]);
    setSessions((current) => upsertSession(current, session));
    if (selectedSessionIdRef.current !== id) {
      return;
    }
    setMessages(visibleMessages(session.messages || []));
    setJobs(jobList);
    setAttachments(attachmentList);
    artifactsRef.current = artifactList;
    setArtifacts(artifactList);
    if (options.revealNewArtifacts && artifactList.some((asset) => !previousArtifactIds.has(asset.id))) {
      revealRightPanel("artifacts");
    }
    setStatus({ tone: "ok", text: "Ready" });
  }

  function selectSession(id: string) {
    selectedSessionIdRef.current = id;
    setSessionId(id);
    setMobileNav(false);
    setAssistantDraft("");
    setLiveUserDraft("");
    setRuntimeError("");
    if (id === sessionId) {
      refreshSessionData(id).catch((error) => showError(error));
    }
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
      selectedSessionIdRef.current = session.id;
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
        const nextSessionId = next[0]?.id || "";
        selectedSessionIdRef.current = nextSessionId;
        setSessionId(nextSessionId);
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
      <Suspense fallback={<AdminConsoleFallback onExit={leaveAdminConsole} />}>
        <AdminConsole
          api={api}
          adminToken={adminToken}
          userLabel={authSession.user.display_name || authSession.user.email}
          onAdminTokenChange={updateAdminToken}
          onExit={leaveAdminConsole}
          onLogout={logout}
        />
      </Suspense>
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
              <button className="session-select" onClick={() => selectSession(session.id)}>
                <span>{sessionTitle(session)}</span>
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
              preview={(asset) => setPreviewAsset({ asset, url: api.artifactURL(asset.id), previewUrl: api.artifactPreviewURL(asset.id) })}
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
      {previewAsset && <PreviewModal asset={previewAsset.asset} url={previewAsset.url} previewUrl={previewAsset.previewUrl} onClose={() => setPreviewAsset(null)} />}
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

function AdminConsoleFallback({ onExit }: { onExit: () => void }) {
  return (
    <main className="admin-shell">
      <aside className="admin-sidebar">
        <div className="admin-brand">
          <BrandLogo />
          <div>
            <strong>AgentAPI Admin</strong>
            <small>Loading console</small>
          </div>
        </div>
        <div className="admin-sidebar-actions">
          <button onClick={onExit}><MessageCircle size={16} /> Back to app</button>
        </div>
      </aside>
      <section className="admin-main">
        <div className="admin-empty">
          <Activity size={26} />
          <strong>Loading admin tools</strong>
          <p>Preparing management panels.</p>
        </div>
      </section>
    </main>
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

function PreviewModal({ asset, url, previewUrl, onClose }: { asset: Asset; url: string; previewUrl?: string; onClose: () => void }) {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  const isImage = isImageAsset(asset);
  const isPDF = isPDFAsset(asset);
  const isText = isTextAsset(asset);
  const isDocx = isDOCXAsset(asset);
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
          {isDocx && previewUrl && <iframe src={previewUrl} title={`${asset.filename} preview`} />}
          {isText && (
            <div className="text-preview" role="document" aria-label={asset.filename}>
              {textPreview.status === "loading" && <div className="preview-fallback">Loading preview...</div>}
              {textPreview.status === "error" && <div className="preview-fallback">{textPreview.error || "Preview failed"}</div>}
              {textPreview.status === "loaded" && <pre>{textPreview.content}</pre>}
            </div>
          )}
          {isOffice && (!isDocx || !previewUrl) && (
            <div className="preview-fallback">
              <FileUp size={32} />
              <strong>{asset.filename}</strong>
              <p>Office previews depend on the browser or deployment viewer. Use download/open for this file.</p>
            </div>
          )}
          {!isImage && !isPDF && !isText && !isDocx && !isOffice && (
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

function isDOCXAsset(asset: Asset): boolean {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return asset.content_type === "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || ext === "docx";
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
