import { FormEvent, lazy, ReactNode, Suspense, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  Database,
  MessageCircle,
  PlayCircle,
  UserX,
  X
} from "lucide-react";
import { ApiClient, ApiError } from "../../api/client";
import type { Asset, AuthSession, Job, JobEvent, MemoryItem, MemoryMaintenanceAction, MemorySettings, Message, MessageSearchResult, PersonalizationSettings, ReadinessStatus, RuntimeEvent, Session, Skill } from "../../types";
import { readSSEStream } from "../../lib/sse";
import { sessionTitle } from "../../lib/sessionTitle";
import { AuthPage, type AuthMode } from "../auth/AuthPage";
import { BrandLogo } from "../../components/brand/BrandLogo";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Textarea } from "../../components/ui/textarea";
import { ConversationPane } from "./components/ConversationPane";
import { ArtifactWorkspace } from "./components/ArtifactWorkspace";
import { MemoryModal } from "./components/MemoryModal";
import { MessageBubble } from "./components/MessageBubble";
import { PreviewModal } from "./components/PreviewModal";
import { SettingsModal } from "./components/SettingsModal";
import { GlobalSearchDialog } from "./components/GlobalSearchDialog";
import { MessageComposer } from "./components/MessageComposer";
import { WorkspaceFrame } from "./components/WorkspaceFrame";
import { WorkspaceResourceDialog } from "./components/WorkspaceResourceDialog";
import { WorkspaceSidebar } from "./components/WorkspaceSidebar";
import { SkillGlyph } from "./components/right-panel/SkillPanel";
import { useLiveVoice } from "./hooks/useLiveVoice";
import type { ComposerToolID, ConfirmDialog, JobStreamStatus, RightPanelSearch, RightPanelTab, ServiceStatus, Status } from "./workspaceTypes";

const AdminConsole = lazy(() => import("../../admin/AdminConsole"));

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

const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);
const terminalRuntimeEvents = new Set(["done", "error", "cancelled"]);
const serviceStatusPollMs = 10_000;
const activeJobStorageKey = "agentapi.activeJob";
const recentSkillsStorageKey = "agentapi.recentSkills";
const adminTokenStorageKey = "agentapi.adminToken";
const jobReconnectBaseMs = 1_000;
const jobReconnectMaxMs = 10_000;
const resourcePageSize = 10;
const resourceTabs: RightPanelTab[] = ["skills", "jobs", "attachments", "artifacts"];

function isAdminPath(): boolean {
  return typeof window !== "undefined" && window.location.pathname.replace(/\/+$/, "") === "/admin";
}

function passwordResetTokenFromLocation(): string {
  if (typeof window === "undefined") return "";
  if (window.location.pathname.replace(/\/+$/, "") !== "/reset-password") return "";
  return new URLSearchParams(window.location.search).get("token") || "";
}

function emptyResourceNotices(): Record<RightPanelTab, boolean> {
  return {
    skills: false,
    jobs: false,
    attachments: false,
    artifacts: false
  };
}

function emptyResourceIdSets(): Record<RightPanelTab, Set<string>> {
  return {
    skills: new Set(),
    jobs: new Set(),
    attachments: new Set(),
    artifacts: new Set()
  };
}

export function AgentWorkspace() {
  const [auth, setAuth] = useState<AuthSession | null>(null);
  const api = useMemo(() => new ApiClient(setAuth), []);
  const [status, setStatus] = useState<Status>({ tone: "idle", text: "Idle" });
  const [serviceStatus, setServiceStatus] = useState<ServiceStatus>({ tone: "busy", text: "Checking" });
  const [passwordResetToken, setPasswordResetToken] = useState(passwordResetTokenFromLocation);
  const [authMode, setAuthMode] = useState<AuthMode>(() => passwordResetTokenFromLocation() ? "reset" : "login");
  const [forgotCooldownSeconds, setForgotCooldownSeconds] = useState(0);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionId, setSessionId] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [online, setOnline] = useState(() => typeof navigator === "undefined" ? true : navigator.onLine);
  const [draft, setDraft] = useState("");
  const [assistantDraft, setAssistantDraft] = useState("");
  const [responseTiming, setResponseTiming] = useState<{ sessionId: string; ttftMs?: number; totalMs?: number } | null>(null);
  const [runtimeError, setRuntimeError] = useState("");
  const [skills, setSkills] = useState<Skill[]>([]);
  const [skillDetail, setSkillDetail] = useState<Skill | null>(null);
  const [selectedComposerTool, setSelectedComposerTool] = useState<ComposerToolID | "">("");
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
  const [artifactWorkspaceOpen, setArtifactWorkspaceOpen] = useState(false);
  const [artifactWorkspaceMounted, setArtifactWorkspaceMounted] = useState(false);
  const [artifactWorkspaceVisible, setArtifactWorkspaceVisible] = useState(false);
  const [artifactWorkspaceAssetId, setArtifactWorkspaceAssetId] = useState("");
  const [globalSearchOpen, setGlobalSearchOpen] = useState(false);
  const [globalSearchQuery, setGlobalSearchQuery] = useState("");
  const [globalSearchResults, setGlobalSearchResults] = useState<MessageSearchResult[]>([]);
  const [globalSearchLoading, setGlobalSearchLoading] = useState(false);
  const [globalSearchError, setGlobalSearchError] = useState("");
  const [globalSearchTarget, setGlobalSearchTarget] = useState<{ sessionID: string; messageIndex: number } | null>(null);
  const [highlightedMessageIndex, setHighlightedMessageIndex] = useState<number | null>(null);
  const [mobileNav, setMobileNav] = useState(false);
  const [busyChat, setBusyChat] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadError, setUploadError] = useState("");
  const [pendingAttachments, setPendingAttachments] = useState<Asset[]>([]);
  const [previewAsset, setPreviewAsset] = useState<{ asset: Asset; loadAsset: () => Promise<Blob>; loadPreview?: () => Promise<Blob> } | null>(null);
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
  const [resourceDialogTab, setResourceDialogTab] = useState<RightPanelTab | null>(null);
  const [resourceSearch, setResourceSearch] = useState<RightPanelSearch>({
    skills: "",
    jobs: "",
    attachments: "",
    artifacts: ""
  });
  const [resourceVisibleCount, setResourceVisibleCount] = useState<Record<RightPanelTab, number>>({
    skills: resourcePageSize,
    jobs: resourcePageSize,
    attachments: resourcePageSize,
    artifacts: resourcePageSize
  });
  const [resourceNotices, setResourceNotices] = useState<Record<RightPanelTab, boolean>>(emptyResourceNotices);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialog | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const attachmentInputRef = useRef<HTMLInputElement | null>(null);
  const jobSourceRef = useRef<EventSource | null>(null);
  const jobReconnectTimerRef = useRef<number | null>(null);
  const jobReconnectAttemptRef = useRef(0);
  const jobStreamClosedRef = useRef(false);
  const activeJobStreamIdRef = useRef("");
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const selectedSessionIdRef = useRef("");
  const resourceDialogTabRef = useRef<RightPanelTab | null>(null);
  const artifactWorkspaceOpenRef = useRef(false);
  const resourceIdsRef = useRef<Record<RightPanelTab, Set<string>>>(emptyResourceIdSets());
  const resourceBaselineReadyRef = useRef<Record<RightPanelTab, boolean>>(emptyResourceNotices());
  const accountRef = useRef<HTMLDivElement | null>(null);
  const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const lastJobEventRef = useRef("");
  const confirmResolverRef = useRef<((confirmed: boolean) => void) | null>(null);
  const artifactsRef = useRef<Asset[]>([]);
  const authSession = auth || api.session();
  const activeSession = sessions.find((item) => item.id === sessionId);
  const latestJob = jobs[0];
  const latestJobId = latestJob?.id || "";
  const activeResourceTab = resourceDialogTab || "skills";
  const activeResourceSearch = resourceSearch[activeResourceTab];
  const activeResourceVisibleCount = resourceVisibleCount[activeResourceTab];
  const selectedWorkspaceArtifact = artifacts.find((asset) => asset.id === artifactWorkspaceAssetId) || artifacts[0] || null;
  const {
    inputMode,
    liveStatus,
    liveMuted,
    speakerVolume: liveSpeakerVolume,
    micVolume: liveMicVolume,
    liveUserDraft,
    stopLiveMode,
    switchToTextMode,
    switchToLiveMode,
    toggleSpeakerMute,
    toggleMicMute,
    setSpeakerVolume: changeLiveSpeakerVolume,
    setMicVolume: changeLiveMicVolume
  } = useLiveVoice({
    api,
    sessionId,
    onRuntimeEvent: handleRuntimeEvent,
    onAssistantDraftChange: setAssistantDraft,
    onLiveSkillMessage: (event) => {
      handleRuntimeEvent(event);
      void refreshSessionData(sessionId, { revealNewArtifacts: true });
    },
    onError: setRuntimeError,
    onStatus: setStatus
  });
  const filteredSkills = useMemo(
    () => skills.filter((skill) => fuzzyMatch(resourceSearch.skills, [
      skill.name,
      skill.display_name,
      skill.description,
      skill.short_description,
      skill.category,
      skill.version,
      ...(skill.tags || [])
    ])),
    [skills, resourceSearch.skills]
  );
  const filteredJobs = useMemo(
    () => jobs.filter((job) => fuzzyMatch(resourceSearch.jobs, [job.id, job.content, job.status, job.type])),
    [jobs, resourceSearch.jobs]
  );
  const filteredAttachments = useMemo(
    () => attachments.filter((asset) => fuzzyMatch(resourceSearch.attachments, [asset.filename, asset.id, asset.content_type])),
    [attachments, resourceSearch.attachments]
  );
  const filteredArtifacts = useMemo(
    () => artifacts.filter((asset) => fuzzyMatch(resourceSearch.artifacts, [asset.filename, asset.id, asset.content_type, asset.job_id])),
    [artifacts, resourceSearch.artifacts]
  );
  const visibleResourceSkills = filteredSkills.slice(0, resourceVisibleCount.skills);
  const visibleResourceJobs = filteredJobs.slice(0, resourceVisibleCount.jobs);
  const visibleResourceAttachments = filteredAttachments.slice(0, resourceVisibleCount.attachments);
  const visibleResourceArtifacts = filteredArtifacts.slice(0, resourceVisibleCount.artifacts);
  const activeResourceTotalCount = resourceDialogTab ? resourceTotalCount(resourceDialogTab, {
    skills: filteredSkills.length,
    jobs: filteredJobs.length,
    attachments: filteredAttachments.length,
    artifacts: filteredArtifacts.length
  }) : 0;
  const recoveryBanner: { tone: "busy" | "error"; text: string } | null = !online
    ? { tone: "error", text: "Network connection lost. New messages may fail until the browser is back online." }
    : selectedJobId && (jobStreamStatus === "reconnecting" || jobStreamStatus === "failed")
      ? { tone: jobStreamStatus === "failed" ? "error" : "busy", text: jobStreamNotice || "Restoring live job updates..." }
      : null;

  useEffect(() => {
    api.start();
    const existing = api.session();
    if (existing) {
      setAuth(existing);
      bootstrap(api, setStatus, setSessions, setSessionId, setMessages, setSkills, setJobs, setAttachments, setArtifacts, baselineFetchedResources).catch((err) => {
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
      api.dispose();
    };
  }, [api]);

  useEffect(() => {
    const onPopState = () => setAdminView(isAdminPath());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    if (forgotCooldownSeconds <= 0) return;
    const timer = window.setTimeout(() => {
      setForgotCooldownSeconds((current) => Math.max(0, current - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [forgotCooldownSeconds]);

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
    resetSessionScopedFeedback();
    refreshSessionData(sessionId).catch((error) => showError(error));
    stopLiveMode(false);
  }, [sessionId]);

  useEffect(() => {
    artifactsRef.current = artifacts;
  }, [artifacts]);

  useEffect(() => {
    resourceDialogTabRef.current = resourceDialogTab;
    if (resourceDialogTab) {
      markResourceViewed(resourceDialogTab);
    }
  }, [resourceDialogTab]);

  useEffect(() => {
    artifactWorkspaceOpenRef.current = artifactWorkspaceOpen;
    if (artifactWorkspaceOpen) markResourceViewed("artifacts");
  }, [artifactWorkspaceOpen]);

  useEffect(() => {
    if (artifactWorkspaceOpen) {
      setArtifactWorkspaceMounted(true);
      const frame = window.requestAnimationFrame(() => setArtifactWorkspaceVisible(true));
      return () => window.cancelAnimationFrame(frame);
    }
    setArtifactWorkspaceVisible(false);
    const timeout = window.setTimeout(() => setArtifactWorkspaceMounted(false), 380);
    return () => window.clearTimeout(timeout);
  }, [artifactWorkspaceOpen]);

  useEffect(() => {
    if (!artifacts.length) {
      setArtifactWorkspaceAssetId("");
      return;
    }
    if (artifactWorkspaceAssetId && artifacts.some((asset) => asset.id === artifactWorkspaceAssetId)) return;
    if (artifactWorkspaceOpen) setArtifactWorkspaceAssetId(artifacts[0].id);
  }, [artifactWorkspaceAssetId, artifactWorkspaceOpen, artifacts]);

  useEffect(() => {
    resetSessionResourceNotices();
  }, [sessionId]);

  useEffect(() => {
    trackResourceIds("skills", skills.map((skill) => `${skill.name}:${skill.version || ""}`));
  }, [skills]);

  useEffect(() => {
    trackResourceIds("jobs", jobs.map((job) => job.id));
  }, [jobs]);

  useEffect(() => {
    trackResourceIds("attachments", attachments.map((asset) => asset.id));
  }, [attachments]);

  useEffect(() => {
    trackResourceIds("artifacts", artifacts.map((asset) => asset.id));
  }, [artifacts]);

  useEffect(() => {
    resizeComposerInput(composerInputRef.current);
  }, [draft]);

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

  async function refreshAll() {
    setServiceStatus(await readServiceStatus(api));
    await bootstrap(api, setStatus, setSessions, setSessionId, setMessages, setSkills, setJobs, setAttachments, setArtifacts, baselineFetchedResources);
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
    if (!resourceBaselineReadyRef.current.jobs) baselineResourceIds("jobs", jobList.map((job) => job.id));
    if (!resourceBaselineReadyRef.current.attachments) baselineResourceIds("attachments", attachmentList.map((asset) => asset.id));
    if (!resourceBaselineReadyRef.current.artifacts) baselineResourceIds("artifacts", artifactList.map((asset) => asset.id));
    setJobs(jobList);
    setAttachments(attachmentList);
    artifactsRef.current = artifactList;
    setArtifacts(artifactList);
    const newArtifact = artifactList.find((asset) => !previousArtifactIds.has(asset.id));
    if (options.revealNewArtifacts && newArtifact) {
      setArtifactWorkspaceAssetId(newArtifact.id);
      setArtifactWorkspaceOpen(true);
      setResourceDialogTab(null);
      markResourceViewed("artifacts");
      setStatus({ tone: "ok", text: "New artifact available" });
      return;
    }
    setStatus({ tone: "ok", text: "Ready" });
  }

  function selectSession(id: string) {
    selectedSessionIdRef.current = id;
    setSessionId(id);
    setMobileNav(false);
    resetSessionScopedFeedback();
    if (id === sessionId) {
      refreshSessionData(id).catch((error) => showError(error));
    }
  }

  function openResourceDialog(tab: RightPanelTab) {
    if (tab === "artifacts") {
      openArtifactWorkspace();
      return;
    }
    markResourceViewed(tab);
    setResourceDialogTab(tab);
    setResourceVisibleCount((current) => ({ ...current, [tab]: resourcePageSize }));
    setMobileNav(false);
  }

  function openArtifactWorkspace(asset?: Asset) {
    markResourceViewed("artifacts");
    setResourceDialogTab(null);
    setArtifactWorkspaceAssetId(asset?.id || artifactWorkspaceAssetId || artifacts[0]?.id || "");
    setArtifactWorkspaceOpen(true);
    setMobileNav(false);
  }

  function closeResourceDialog(open: boolean) {
    if (!open) {
      setResourceDialogTab(null);
      return;
    }
    setResourceDialogTab((current) => current || "skills");
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

  function changeAuthMode(mode: AuthMode) {
    setAuthMode(mode);
    if (mode !== "reset") {
      setPasswordResetToken("");
      if (typeof window !== "undefined" && window.location.pathname.replace(/\/+$/, "") === "/reset-password") {
        window.history.replaceState({}, "", "/");
      }
    }
    setStatus({ tone: "idle", text: mode === "forgot" ? "Enter your account email" : "Idle" });
  }

  async function submitAuth(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const email = String(form.get("email") || "").trim();
    const password = String(form.get("password") || "");
    const confirmPassword = String(form.get("confirmPassword") || "");
    const displayName = String(form.get("displayName") || "").trim();
    if ((authMode === "register" || authMode === "reset") && password !== confirmPassword) {
      setStatus({ tone: "error", text: "Passwords do not match" });
      return;
    }
    if (authMode === "reset" && !passwordResetToken) {
      setStatus({ tone: "error", text: "Reset link is missing or expired" });
      return;
    }
    if (authMode === "forgot" && forgotCooldownSeconds > 0) {
      setStatus({ tone: "ok", text: `Reset email already sent. Try again in ${forgotCooldownSeconds}s.` });
      return;
    }
    setStatus({
      tone: "busy",
      text: authMode === "register"
        ? "Creating account and sending verification email"
        : authMode === "forgot"
          ? "Sending password reset email"
          : authMode === "reset"
            ? "Resetting password"
            : "Signing in"
    });
    try {
      if (authMode === "register") {
        const result = await api.register(email, password, displayName);
        if ("verification_required" in result && result.verification_required) {
          changeAuthMode("login");
          setStatus({ tone: "ok", text: `Verification email sent to ${result.email}. Check your inbox and spam folder.` });
          return;
        }
      } else if (authMode === "forgot") {
        await api.requestPasswordReset(email);
        setForgotCooldownSeconds(60);
        setStatus({ tone: "ok", text: "If an account exists, a reset link has been sent. Check your inbox and spam folder." });
        return;
      } else if (authMode === "reset") {
        await api.resetPassword(passwordResetToken, password);
        changeAuthMode("login");
        setStatus({ tone: "ok", text: "Password reset. Sign in with your new password." });
        return;
      } else {
        await api.login(email, password);
      }
      await refreshAll();
    } catch (error) {
      if (authMode === "register" || authMode === "forgot") {
        const message = authMode === "register"
          ? `Could not send verification email: ${errorMessage(error)}`
          : `Could not send password reset email: ${errorMessage(error)}`;
        setRuntimeError(message);
        setStatus({ tone: "error", text: message });
        readServiceStatus(api).then(setServiceStatus).catch(() => {});
      } else if (authMode === "reset") {
        const message = `Could not reset password: ${errorMessage(error)}`;
        setRuntimeError(message);
        setStatus({ tone: "error", text: message });
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
      resetSessionScopedFeedback({ clearDraft: true });
    } catch (error) {
      showError(error);
    }
  }

  async function removeSession(targetSessionId: string) {
    if (!targetSessionId) return;
    const targetSession = sessions.find((item) => item.id === targetSessionId);
    const targetTitle = targetSession ? sessionTitle(targetSession) || targetSessionId : targetSessionId;
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
        resetSessionScopedFeedback({ clearDraft: true });
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
    const requestSessionId = sessionId;
    const attachmentIds = pendingAttachments.map((asset) => asset.id);
    const abort = new AbortController();
    let routedToJob = false;
    let sawRuntimeError = false;
    let firstTokenSeen = false;
    const startedAt = performance.now();
    abortRef.current = abort;
    setDraft("");
    setAssistantDraft("");
    setResponseTiming(null);
    setRuntimeError("");
    const displayContent = content || "Please analyze the attached file(s).";
    const requestContent = composerToolContent(selectedComposerTool, displayContent);
    const sentMessage: Message = { role: "user", content: messageWithAttachmentNames(displayContent, pendingAttachments), created_at: new Date().toISOString() };
    setMessages((current) => appendRuntimeMessage(current, sentMessage));
    setSessions((current) => current.map((item) => {
      if (item.id !== requestSessionId || sessionTitle(item)) return item;
      return { ...item, messages: appendRuntimeMessage(item.messages || [], sentMessage) };
    }));
    setBusyChat(true);
    setStatus({ tone: "busy", text: "Generating" });
    try {
      const response = await api.chatResponse(requestSessionId, requestContent, attachmentIds, abort.signal);
      await readSSEStream(response, ({ data }) => {
        if (data.type === "job") routedToJob = true;
        if (data.type === "error") sawRuntimeError = true;
        if (!firstTokenSeen && data.type === "delta" && data.content) {
          firstTokenSeen = true;
          const ttftMs = Math.max(0, Math.round(performance.now() - startedAt));
          if (selectedSessionIdRef.current === requestSessionId) {
            setResponseTiming({ sessionId: requestSessionId, ttftMs });
            setStatus({ tone: "busy", text: `First response ${formatNumber(ttftMs)} ms` });
          }
        }
        if (selectedSessionIdRef.current === requestSessionId) {
          handleRuntimeEvent(data);
        }
      });
      if (selectedSessionIdRef.current === requestSessionId) {
        setResponseTiming((current) => current?.sessionId === requestSessionId ? { ...current, totalMs: Math.max(0, Math.round(performance.now() - startedAt)) } : current);
      }
      setPendingAttachments([]);
      if (!routedToJob) await refreshSessionData(requestSessionId, { revealNewArtifacts: true });
      if (sawRuntimeError) {
        setStatus((current) => current.tone === "error" ? current : { tone: "error", text: "Request failed" });
      }
    } catch (error) {
      if ((error as Error).name !== "AbortError") {
        const message = errorMessage(error);
        if (selectedSessionIdRef.current === requestSessionId) {
          setRuntimeError(`Message delivery failed. Your sent message was kept in the conversation. ${message}`);
          setStatus({ tone: "error", text: "Message failed" });
          setMessages((current) => appendRuntimeMessage(current, {
            role: "assistant",
            content: `Message delivery failed. Your sent message was kept here, but the response stream did not finish.\n\n${message}`,
            created_at: new Date().toISOString()
          }));
        }
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

  function resetSessionScopedFeedback(options: { clearDraft?: boolean } = {}) {
    setAssistantDraft("");
    setRuntimeError("");
    setUploadError("");
    setResponseTiming(null);
    setPendingAttachments([]);
    if (options.clearDraft) {
      setDraft("");
    }
  }

  async function uploadAttachment(fileList: FileList | null) {
    const file = fileList?.[0];
    if (!file) return;
    setUploading(true);
    setUploadError("");
    setUploadProgress(0);
    setStatus({ tone: "busy", text: "Uploading" });
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

  function previewAttachment(asset: Asset) {
    setPreviewAsset({
      asset,
      loadAsset: () => api.attachmentBlob(asset.id)
    });
  }

  function previewArtifact(asset: Asset) {
    setPreviewAsset({
      asset,
      loadAsset: () => api.artifactBlob(asset.id),
      loadPreview: () => api.artifactPreviewBlob(asset.id)
    });
  }

  async function downloadAttachment(id: string) {
    const asset = attachments.find((item) => item.id === id);
    await downloadAsset(() => api.attachmentBlob(id), asset?.filename || id);
  }

  async function downloadArtifact(id: string) {
    const asset = artifacts.find((item) => item.id === id);
    await downloadAsset(() => api.artifactBlob(id), asset?.filename || id);
  }

  async function downloadAsset(loadAsset: () => Promise<Blob>, filename: string) {
    setStatus({ tone: "busy", text: "Preparing download" });
    try {
      const blob = await loadAsset();
      const url = URL.createObjectURL(blob);
      try {
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = filename;
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
      } finally {
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      }
      setStatus({ tone: "ok", text: "Download ready" });
    } catch (error) {
      showError(error);
    }
  }

  function insertSkill(skill: Skill) {
    const command = `/${skill.name} `;
    setDraft(command);
    setSelectedComposerTool("");
    switchToTextMode();
    rememberRecentSkill(skill);
    setStatus({ tone: "ok", text: `Inserted /${skill.name}` });
    composerInputRef.current?.focus();
    setSkillDetail(null);
    setResourceDialogTab(null);
  }

  function rememberRecentSkill(skill: Skill) {
    setRecentSkillNames((current) => {
      const next = [skill.name, ...current.filter((name) => name !== skill.name)].slice(0, 6);
      writeRecentSkills(next);
      return next;
    });
  }

  function selectChatMode() {
    setSelectedComposerTool("");
    switchToTextMode();
    setStatus({ tone: "idle", text: "Chat mode" });
    composerInputRef.current?.focus();
  }

  function selectComposerTool(toolId: ComposerToolID) {
    const nextTool = selectedComposerTool === toolId ? "" : toolId;
    setSelectedComposerTool(nextTool);
    switchToTextMode();
    setStatus({ tone: nextTool ? "ok" : "idle", text: nextTool ? composerToolStatus(nextTool) : "Chat mode" });
    composerInputRef.current?.focus();
  }

  function selectLiveMode() {
    setSelectedComposerTool("");
    switchToLiveMode();
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

  function updateResourceSearch(value: string) {
    setResourceSearch((current) => ({ ...current, [activeResourceTab]: value }));
    setResourceVisibleCount((current) => ({ ...current, [activeResourceTab]: resourcePageSize }));
  }

  function changeResourceTab(tab: RightPanelTab) {
    if (tab === "artifacts") {
      openArtifactWorkspace();
      return;
    }
    markResourceViewed(tab);
    setResourceDialogTab(tab);
    setResourceVisibleCount((current) => ({ ...current, [tab]: resourcePageSize }));
  }

  function trackResourceIds(tab: RightPanelTab, ids: string[]) {
    const nextIds = new Set(ids);
    const wasReady = resourceBaselineReadyRef.current[tab];
    const previousIds = resourceIdsRef.current[tab];
    if (!wasReady) {
      resourceIdsRef.current[tab] = nextIds;
      if (ids.length > 0) resourceBaselineReadyRef.current[tab] = true;
      return;
    }
    const hasNewItem = wasReady && ids.some((id) => !previousIds.has(id));
    resourceIdsRef.current[tab] = nextIds;
    resourceBaselineReadyRef.current[tab] = true;
    if (!hasNewItem) return;
    if (resourceDialogTabRef.current === tab || (tab === "artifacts" && artifactWorkspaceOpenRef.current)) {
      markResourceViewed(tab);
      return;
    }
    setResourceNotices((current) => current[tab] ? current : { ...current, [tab]: true });
  }

  function markResourceViewed(tab: RightPanelTab) {
    setResourceNotices((current) => current[tab] ? { ...current, [tab]: false } : current);
  }

  function baselineResourceIds(tab: RightPanelTab, ids: string[]) {
    resourceIdsRef.current[tab] = new Set(ids);
    resourceBaselineReadyRef.current[tab] = true;
  }

  function baselineFetchedResources(resources: {
    skills: Skill[];
    jobs: Job[];
    attachments: Asset[];
    artifacts: Asset[];
  }) {
    baselineResourceIds("skills", resources.skills.map((skill) => `${skill.name}:${skill.version || ""}`));
    baselineResourceIds("jobs", resources.jobs.map((job) => job.id));
    baselineResourceIds("attachments", resources.attachments.map((asset) => asset.id));
    baselineResourceIds("artifacts", resources.artifacts.map((asset) => asset.id));
    setResourceNotices(emptyResourceNotices());
  }

  function resetSessionResourceNotices() {
    for (const tab of resourceTabs) {
      if (tab === "skills") continue;
      resourceIdsRef.current[tab] = new Set();
      resourceBaselineReadyRef.current[tab] = false;
    }
    setResourceNotices(emptyResourceNotices());
  }

  function loadMoreResources() {
    setResourceVisibleCount((current) => ({
      ...current,
      [activeResourceTab]: current[activeResourceTab] + resourcePageSize
    }));
  }

  function leaveAdminConsole() {
    window.history.pushState({}, "", "/");
    setAdminView(false);
  }

  if (!authSession) {
    return (
      <AuthPage
        mode={authMode}
        status={status}
        forgotCooldownSeconds={forgotCooldownSeconds}
        onModeChange={changeAuthMode}
        onSubmit={submitAuth}
        statusLine={(nextStatus) => <StatusLine status={nextStatus} />}
      />
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
    <WorkspaceFrame
      leftCollapsed={!leftSidebarOpen}
      sidebar={(
        <WorkspaceSidebar
          authSession={authSession}
          sessions={sessions}
          sessionId={sessionId}
          mobileOpen={mobileNav}
          leftOpen={leftSidebarOpen}
          serviceStatus={serviceStatus}
          settingsOpen={settingsOpen}
          accountRef={accountRef}
          resourceCounts={{
            skills: skills.length,
            jobs: jobs.length,
            attachments: attachments.length,
            artifacts: artifacts.length
          }}
          resourceNotices={resourceNotices}
          serviceStatusPill={(nextStatus) => <ServiceStatusPill status={nextStatus} />}
          onToggleLeft={() => {
            setGlobalSearchOpen(false);
            setLeftSidebarOpen((open) => !open);
          }}
          onCollapseLeft={() => {
            setGlobalSearchOpen(false);
            setLeftSidebarOpen(false);
          }}
          onCloseMobile={() => setMobileNav(false)}
          onCreateSession={createSession}
          onOpenSearch={() => setGlobalSearchOpen(true)}
          onOpenResource={openResourceDialog}
          onSelectSession={selectSession}
          onRemoveSession={removeSession}
          onToggleSettings={setSettingsOpen}
          onOpenSettings={() => {
            setSettingsOpen(false);
            setSettingsModalOpen(true);
          }}
          onManageMemory={() => openMemoryManager("all")}
          onLogout={logout}
        />
      )}
      workspace={(
        <div className={`workspace-stage ${artifactWorkspaceMounted ? "with-artifact-workspace" : ""}`}>
          <ConversationPane
            activeSession={activeSession}
            status={status}
            recoveryBanner={recoveryBanner}
            online={online}
            selectedJobId={selectedJobId}
            userLabel={authSession.user.display_name || authSession.user.email}
            messages={messages}
            liveUserDraft={liveUserDraft}
            assistantDraft={assistantDraft}
            highlightedMessageIndex={highlightedMessageIndex}
            messagesRef={messagesRef}
            statusLine={(nextStatus) => <StatusLine status={nextStatus} />}
            messageBubble={(props) => <MessageBubble {...props} />}
            onOpenMobileNav={() => setMobileNav(true)}
            onReconnectJob={reconnectSelectedJob}
            composer={(
              <MessageComposer
                runtimeError={runtimeError}
                uploadError={uploadError}
                responseTiming={responseTiming?.sessionId === sessionId ? responseTiming : null}
                pendingAttachments={pendingAttachments}
                selectedToolId={selectedComposerTool}
                attachmentInputRef={attachmentInputRef}
                composerInputRef={composerInputRef}
                uploading={uploading}
                inputMode={inputMode}
                liveStatus={liveStatus}
                liveMuted={liveMuted}
                liveSpeakerVolume={liveSpeakerVolume}
                liveMicVolume={liveMicVolume}
                busyChat={busyChat}
                sessionId={sessionId}
                draft={draft}
                onClearRuntimeError={() => setRuntimeError("")}
                onClearUploadError={() => setUploadError("")}
                onRemovePendingAttachment={(id) => setPendingAttachments((current) => current.filter((item) => item.id !== id))}
                onUploadAttachment={uploadAttachment}
                onDraftChange={setDraft}
                onSendMessage={sendMessage}
                onCancelChat={cancelChat}
                onSelectChatMode={selectChatMode}
                onSwitchToLive={selectLiveMode}
                onSelectTool={selectComposerTool}
                onToggleLiveMute={toggleSpeakerMute}
                onToggleLiveCapture={() => void toggleMicMute()}
                onLiveSpeakerVolumeChange={changeLiveSpeakerVolume}
                onLiveMicVolumeChange={(value) => void changeLiveMicVolume(value)}
                formatNumber={formatNumber}
              />
            )}
          />
          {artifactWorkspaceMounted && (
            <ArtifactWorkspace
              className={artifactWorkspaceVisible ? "visible" : ""}
              artifacts={artifacts}
              selectedArtifactId={selectedWorkspaceArtifact?.id || ""}
              memoryBusy={assetMemoryBusy}
              memoryDisabled={!memorySettings.capture_enabled}
              onSelectArtifact={setArtifactWorkspaceAssetId}
              onClose={() => setArtifactWorkspaceOpen(false)}
              onOpenPreview={previewArtifact}
              onDownload={(id) => { void downloadArtifact(id); }}
              onDelete={(id) => deleteAsset("artifact", id)}
              onExtractMemory={extractAssetMemory}
              loadArtifact={(asset) => api.artifactBlob(asset.id)}
              loadPreview={(asset) => api.artifactPreviewBlob(asset.id)}
              formatBytes={formatBytes}
              formatTime={formatTime}
            />
          )}
        </div>
      )}
      modals={(
        <>
          <WorkspaceResourceDialog
            open={Boolean(resourceDialogTab)}
            activeTab={activeResourceTab}
            searchValue={activeResourceSearch}
            visibleCount={activeResourceVisibleCount}
            totalCount={activeResourceTotalCount}
            skills={visibleResourceSkills}
            recentSkillNames={recentSkillNames}
            jobs={visibleResourceJobs}
            selectedJobId={selectedJobId}
            jobEvents={jobEvents}
            jobStreamNotice={jobStreamNotice}
            jobStreamStatus={jobStreamStatus}
            attachments={visibleResourceAttachments}
            artifacts={visibleResourceArtifacts}
            uploadProgress={uploadProgress}
            assetMemoryBusy={assetMemoryBusy}
            memoryDisabled={!memorySettings.capture_enabled}
            resourceNotices={resourceNotices}
            onOpenChange={closeResourceDialog}
            onTabChange={changeResourceTab}
            onSearchChange={updateResourceSearch}
            onLoadMore={loadMoreResources}
            onInsertSkill={insertSkill}
            onSkillDetails={setSkillDetail}
            onToggleJob={toggleJob}
            onCancelJob={cancelJob}
            onPreviewAttachment={previewAttachment}
            onDownloadAttachment={(id) => { void downloadAttachment(id); }}
            onDeleteAttachment={(id) => deleteAsset("attachment", id)}
            onAddAttachmentToMessage={addAttachmentToMessage}
            onPreviewArtifact={previewArtifact}
            onDownloadArtifact={(id) => { void downloadArtifact(id); }}
            onDeleteArtifact={(id) => deleteAsset("artifact", id)}
            onExtractMemory={extractAssetMemory}
            formatBytes={formatBytes}
            formatTime={formatTime}
          />
          <GlobalSearchDialog
            open={globalSearchOpen}
            query={globalSearchQuery}
            loading={globalSearchLoading}
            error={globalSearchError}
            results={globalSearchResults}
            onOpenChange={setGlobalSearchOpen}
            onQueryChange={setGlobalSearchQuery}
            onOpenResult={openSearchResult}
            formatTime={formatTime}
          />
          {skillDetail && (
            <SkillDetailModal
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
              onStatusChange={(nextStatus) => {
                setMemoryStatusFilter(nextStatus);
                void loadMemoryItems(memoryScope, nextStatus, memoryLevelFilter);
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
          {previewAsset && (
            <PreviewModal
              asset={previewAsset.asset}
              loadAsset={previewAsset.loadAsset}
              loadPreview={previewAsset.loadPreview}
              onClose={() => setPreviewAsset(null)}
            />
          )}
          {confirmDialog && (
            <ConfirmModal
              dialog={confirmDialog}
              onCancel={() => resolveConfirmation(false)}
              onConfirm={() => resolveConfirmation(true)}
            />
          )}
        </>
      )}
    />
  );
}

export default AgentWorkspace;

async function bootstrap(
  api: ApiClient,
  setStatus: (status: Status) => void,
  setSessions: (sessions: Session[]) => void,
  setSessionId: (sessionId: string) => void,
  setMessages: (messages: Message[]) => void,
  setSkills: (skills: Skill[]) => void,
  setJobs: (jobs: Job[]) => void,
  setAttachments: (assets: Asset[]) => void,
  setArtifacts: (assets: Asset[]) => void,
  baselineResources?: (resources: {
    skills: Skill[];
    jobs: Job[];
    attachments: Asset[];
    artifacts: Asset[];
  }) => void
) {
  setStatus({ tone: "busy", text: "Loading" });
  let sessionList = await api.sessions();
  if (!sessionList.length) {
    const session = await api.createSession();
    sessionList = [session];
  }
  const storedJob = loadActiveJob();
  const currentSummary = sessionList.find((session) => session.id === storedJob?.sessionId) || sessionList[0];
  const [current, skills, jobs, attachments, artifacts] = await Promise.all([
    api.getSession(currentSummary.id),
    api.skills(),
    api.jobs(currentSummary.id),
    api.attachments(currentSummary.id),
    api.artifacts(currentSummary.id)
  ]);
  baselineResources?.({ skills, jobs, attachments, artifacts });
  setSessions(upsertSession(sessionList, current));
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

function composerToolContent(toolId: ComposerToolID | "", content: string): string {
  const trimmed = content.trim();
  if (!toolId || isSlashSkillCommand(trimmed)) return trimmed;
  if (toolId === "image") return ["/vertex-image-artifact", trimmed].filter(Boolean).join(" ");
  if (toolId === "web-search") return `请使用网页搜索查找最新资料，并基于可靠来源回答：${trimmed}`;
  if (toolId === "thinking") return `请先深入思考并检查推理，再回答：${trimmed}`;
  return trimmed;
}

function composerToolStatus(toolId: ComposerToolID): string {
  if (toolId === "image") return "Image generation ready";
  if (toolId === "web-search") return "Web search ready";
  return "Thinking mode ready";
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

function resourceTotalCount(tab: RightPanelTab, counts: Record<RightPanelTab, number>): number {
  return counts[tab];
}

function messageWithAttachmentNames(content: string, assets: Asset[]): string {
  if (assets.length === 0) return content;
  const names = assets.map((asset) => asset.filename).join(", ");
  return `${content}\n\nAttachments: ${names}`;
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
    return "已开始执行工作流，完成后会自动更新结果。你也可以从左侧 Jobs 查看进度。";
  }
  return "已开始后台处理，完成后会自动更新结果。你也可以从左侧 Jobs 查看进度。";
}

function shouldDisplayJobSubmittedContent(event: RuntimeEvent): boolean {
  if (event.job_reason === "skill metadata requests durable job execution") {
    return false;
  }
  return true;
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
          <Button onClick={onExit}><MessageCircle size={16} /> Back to app</Button>
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
  skill,
  onInsert,
  onClose
}: {
  skill: Skill;
  onInsert: (skill: Skill) => void;
  onClose: () => void;
}) {
  const title = skill.display_name || skill.name;
  const examples = skill.usage_examples || [];
  const outputTypes = skill.output_artifact_types || [];
  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onClose();
    }}>
      <DialogContent className="skill-modal" hideClose>
        <DialogTitle className="sr-only">{title}</DialogTitle>
        <DialogDescription className="sr-only">Review skill details and apply the skill to the current message.</DialogDescription>
        <header>
          <div className="skill-modal-heading">
            <SkillGlyph skill={skill} />
            <div>
              <h2 id="skill-modal-title">{title}</h2>
              <small>/{skill.name}</small>
            </div>
          </div>
          <Button className="icon ghost" onClick={onClose} aria-label="Close skill details" title="Close">
            <X size={18} />
          </Button>
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
          <Button className="primary skill-modal-insert" onClick={() => onInsert(skill)}>
            <PlayCircle size={16} />
            <span>Apply /{skill.name}</span>
          </Button>
        </footer>
      </DialogContent>
    </Dialog>
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

function ConfirmModal({
  dialog,
  onCancel,
  onConfirm
}: {
  dialog: ConfirmDialog;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onCancel();
    }}>
      <DialogContent className="confirm-modal shadcn-confirm" hideClose>
        <DialogHeader>
          <DialogTitle>{dialog.title}</DialogTitle>
          <DialogDescription>{dialog.message}</DialogDescription>
          {dialog.detail && <small>{dialog.detail}</small>}
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>{dialog.cancelLabel || "Cancel"}</Button>
          <Button type="button" variant={dialog.danger ? "destructive" : "primary"} onClick={onConfirm}>
            {dialog.confirmLabel || "OK"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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

function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return new Intl.NumberFormat().format(value);
}
