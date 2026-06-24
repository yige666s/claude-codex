import { Dispatch, SetStateAction, useEffect, useRef, useState } from "react";
import { ApiClient, ApiError } from "../../../api/client";
import { userFacingErrorMessage } from "../../../api/errorMessages";
import type { RuntimeEvent } from "../../../types";
import type { Status } from "../workspaceTypes";
import { liveTranscriptNoise } from "../liveTranscriptNoiseConfig";

export type InputMode = "text" | "live";
export type LiveStatus = "idle" | "connecting" | "listening" | "speaking" | "thinking" | "responding" | "paused" | "reconnecting" | "error";

type UseLiveVoiceOptions = {
  api: ApiClient;
  sessionId: string;
  onRuntimeEvent: (event: RuntimeEvent) => void;
  onAssistantDraftChange: Dispatch<SetStateAction<string>>;
  onLiveSkillMessage?: (event: RuntimeEvent) => void;
  onError: (message: string) => void;
  onStatus: (status: Status | ((current: Status) => Status)) => void;
};

const liveVoiceProcessorBufferSize = 1024;
const liveTargetSampleRate = 16000;
const liveModeSwitchCooldownMs = 450;
const liveSocketBackpressureBytes = 512 * 1024;
const livePreSpeechFrameLimit = 6;
const liveSpeechStartMinMs = 260;
const liveEndOfSpeechMs = 700;
const liveCalibrationFrameCount = 16;
const liveReconnectDelayMs = 700;
const liveMaxReconnectAttempts = 2;
const livePrewarmDelayMs = 1500;
const livePrewarmIdleMs = 75_000;
const liveAudioConstraints: MediaTrackConstraints = {
  channelCount: 1,
  echoCancellation: true,
  noiseSuppression: true,
  autoGainControl: true
};

export function useLiveVoice({
  api,
  sessionId,
  onRuntimeEvent,
  onAssistantDraftChange,
  onLiveSkillMessage,
  onError,
  onStatus
}: UseLiveVoiceOptions) {
  const [inputMode, setInputMode] = useState<InputMode>("text");
  const [liveStatus, setLiveStatus] = useState<LiveStatus>("idle");
  const [liveUserDraft, setLiveUserDraft] = useState("");

  const liveSocketRef = useRef<WebSocket | null>(null);
  const liveMediaRef = useRef<MediaStream | null>(null);
  const liveAudioContextRef = useRef<AudioContext | null>(null);
  const liveProcessorRef = useRef<ScriptProcessorNode | null>(null);
  const liveSourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const livePlaybackContextRef = useRef<AudioContext | null>(null);
  const livePlaybackGainRef = useRef<GainNode | null>(null);
  const livePlaybackTimeRef = useRef(0);
  const livePlaybackGenerationRef = useRef(0);
  const livePlaybackSourcesRef = useRef<Set<AudioBufferSourceNode>>(new Set());
  const liveStatusRef = useRef(liveStatus);
  const inputModeRef = useRef(inputMode);
  const liveSessionGenerationRef = useRef(0);
  const liveCaptureGenerationRef = useRef(0);
  const liveAudioChunkCountRef = useRef(0);
  const liveAudioBytesSentRef = useRef(0);
  const livePlaybackQueueRef = useRef(Promise.resolve());
  const liveManualStopRef = useRef(false);
  const liveExpectedCloseRef = useRef(false);
  const liveInputPausedRef = useRef(false);
  const liveReconnectAttemptsRef = useRef(0);
  const liveReconnectTimerRef = useRef<number | null>(null);
  const liveResumeCaptureTimerRef = useRef<number | null>(null);
  const livePrewarmTimerRef = useRef<number | null>(null);
  const livePrewarmIdleTimerRef = useRef<number | null>(null);
  const liveCaptureRequestedRef = useRef(false);
  const liveSetupCompleteRef = useRef(false);
  const liveSwitchLockedUntilRef = useRef(0);
  const liveSessionStartedAtRef = useRef(0);
  const liveSetupAtRef = useRef(0);
  const liveFirstInputTranscriptAtRef = useRef(0);
  const liveFirstOutputTranscriptAtRef = useRef(0);
  const liveFirstAudioAtRef = useRef(0);
  const liveResumptionHandleRef = useRef<string | null>(null);

  useEffect(() => {
    inputModeRef.current = inputMode;
  }, [inputMode]);

  useEffect(() => {
    liveStatusRef.current = liveStatus;
  }, [liveStatus]);

  useEffect(() => {
    if (!navigator.mediaDevices?.addEventListener) return;
    const handleDeviceChange = () => {
      if (inputModeRef.current !== "live") return;
      onStatus({ tone: "busy", text: "Audio device changed; checking microphone" });
      const socket = liveSocketRef.current;
      const generation = liveSessionGenerationRef.current;
      if (!socket || socket.readyState !== WebSocket.OPEN || liveStatusRef.current === "connecting" || liveStatusRef.current === "reconnecting") return;
      stopLiveCapture();
      window.setTimeout(() => {
        if (isCurrentLiveSession(socket, generation)) {
          void startLiveCapture(socket, generation);
        }
      }, 160);
    };
    navigator.mediaDevices.addEventListener("devicechange", handleDeviceChange);
    return () => navigator.mediaDevices.removeEventListener("devicechange", handleDeviceChange);
  }, []);

  useEffect(() => {
    liveResumptionHandleRef.current = null;
    if (!sessionId || typeof sessionStorage === "undefined") return;
    liveResumptionHandleRef.current = sessionStorage.getItem(liveResumptionStorageKey(sessionId));
  }, [sessionId]);

  useEffect(() => {
    clearLivePrewarmTimer();
    if (!sessionId) return;
    livePrewarmTimerRef.current = window.setTimeout(() => {
      livePrewarmTimerRef.current = null;
      if (document.visibilityState !== "visible") return;
      if (inputModeRef.current !== "text" || liveSocketRef.current || liveStatusRef.current !== "idle") return;
      void prewarmLiveMode();
    }, livePrewarmDelayMs);
    return () => {
      clearLivePrewarmTimer();
    };
  }, [sessionId]);

  useEffect(() => {
    return () => {
      clearLiveReconnectTimer();
      clearLiveResumeCaptureTimer();
      clearLivePrewarmTimer();
      clearLivePrewarmIdleTimer();
      stopLiveMode(false, false);
    };
  }, []);

  function updateLiveStatus(next: LiveStatus) {
    liveStatusRef.current = next;
    setLiveStatus(next);
  }

  async function startLiveMode() {
    if (performance.now() < liveSwitchLockedUntilRef.current) return;
    liveCaptureRequestedRef.current = true;
    await connectLiveSocket(true);
  }

  async function prewarmLiveMode() {
    if (performance.now() < liveSwitchLockedUntilRef.current) return;
    if (inputModeRef.current === "live") return;
    await connectLiveSocket(false);
  }

  async function connectLiveSocket(captureRequested: boolean) {
    if (!sessionId) return;
    liveCaptureRequestedRef.current = captureRequested || liveCaptureRequestedRef.current;
    if (typeof WebSocket === "undefined" || (captureRequested && !navigator.mediaDevices?.getUserMedia)) {
      if (captureRequested) {
        onError("Live voice is unavailable in this browser.");
        updateLiveStatus("error");
      }
      return;
    }
    if (captureRequested) {
      const permission = await microphonePermissionState();
      if (permission === "denied") {
        onError("Microphone permission is blocked. Allow microphone access in the browser and try Live again.");
        updateLiveStatus("error");
        onStatus({ tone: "error", text: "Microphone permission blocked" });
        return;
      }
      enterLiveCaptureMode();
      const existing = liveSocketRef.current;
      const generation = liveSessionGenerationRef.current;
      if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
        clearLivePrewarmIdleTimer();
        if (liveSetupCompleteRef.current && existing.readyState === WebSocket.OPEN) {
          await startLiveCaptureWhenReady(existing, generation);
        } else {
          updateLiveStatus("connecting");
          onStatus({ tone: "busy", text: "Connecting live voice" });
        }
        return;
      }
    } else if (liveSocketRef.current || liveStatusRef.current !== "idle") {
      return;
    }
    closeLiveSocket(false);
    const generation = ++liveSessionGenerationRef.current;
    liveSetupCompleteRef.current = false;
    liveManualStopRef.current = false;
    liveExpectedCloseRef.current = false;
    liveInputPausedRef.current = false;
    liveReconnectAttemptsRef.current = 0;
    liveSessionStartedAtRef.current = performance.now();
    liveSetupAtRef.current = 0;
    liveFirstInputTranscriptAtRef.current = 0;
    liveFirstOutputTranscriptAtRef.current = 0;
    liveFirstAudioAtRef.current = 0;
    liveAudioChunkCountRef.current = 0;
    liveAudioBytesSentRef.current = 0;
    if (captureRequested) {
      updateLiveStatus("connecting");
      onStatus({ tone: "busy", text: "Connecting live voice" });
    }
    const resumeHandle = liveResumptionHandleRef.current;
    const socket = new WebSocket(api.liveSessionURL(sessionId, resumeHandle), api.webSocketProtocols());
    liveSocketRef.current = socket;
    socket.onmessage = (message) => {
      if (!isCurrentLiveSocket(socket, generation)) return;
      try {
        void handleLiveRuntimeEvent(JSON.parse(message.data) as RuntimeEvent, socket, generation);
      } catch {
        // Ignore malformed live frames; the socket error handler covers transport failures.
      }
    };
    socket.onerror = () => {
      if (!isCurrentLiveSocket(socket, generation)) return;
      if (inputModeRef.current === "live" || liveCaptureRequestedRef.current) {
        onStatus({ tone: "error", text: "Live voice connection interrupted" });
      }
    };
    socket.onclose = () => {
      if (!isCurrentLiveSocket(socket, generation)) return;
      cleanupLiveAudio();
      liveSocketRef.current = null;
      const setupCompleted = liveSetupCompleteRef.current;
      liveSetupCompleteRef.current = false;
      clearLivePrewarmIdleTimer();
      if (resumeHandle && !setupCompleted && !liveManualStopRef.current && !liveExpectedCloseRef.current) {
        liveResumptionHandleRef.current = null;
        if (sessionId && typeof sessionStorage !== "undefined") {
          sessionStorage.removeItem(liveResumptionStorageKey(sessionId));
        }
        liveStatusRef.current = "idle";
        setLiveStatus("idle");
        void connectLiveSocket(liveCaptureRequestedRef.current);
        return;
      }
      const shouldReconnect = !liveManualStopRef.current && !liveExpectedCloseRef.current && inputModeRef.current === "live" && liveCaptureRequestedRef.current;
      if (shouldReconnect) {
        void scheduleLiveReconnect(generation);
        return;
      }
      liveCaptureRequestedRef.current = false;
      if (inputModeRef.current === "live") {
        updateLiveStatus("idle");
        onStatus((current) => current.tone === "error" ? current : { tone: "idle", text: "Live voice stopped" });
      }
    };
  }

  function enterLiveCaptureMode() {
    clearLivePrewarmTimer();
    clearLivePrewarmIdleTimer();
    setInputMode("live");
    inputModeRef.current = "live";
    onError("");
    onAssistantDraftChange("");
    setLiveUserDraft("");
    stopLivePlayback();
  }

  function stopLiveMode(sendEnd = true, lockSwitch = true) {
    if (lockSwitch) {
      liveSwitchLockedUntilRef.current = performance.now() + liveModeSwitchCooldownMs;
    }
    liveManualStopRef.current = true;
    liveExpectedCloseRef.current = true;
    liveInputPausedRef.current = false;
    clearLiveReconnectTimer();
    clearLiveResumeCaptureTimer();
    clearLivePrewarmTimer();
    clearLivePrewarmIdleTimer();
    liveCaptureRequestedRef.current = false;
    liveSetupCompleteRef.current = false;
    liveSessionGenerationRef.current += 1;
    liveCaptureGenerationRef.current += 1;
    setInputMode("text");
    inputModeRef.current = "text";
    cleanupLiveAudio();
    closeLiveSocket(sendEnd);
    liveStatusRef.current = "idle";
    updateLiveStatus("idle");
    setLiveUserDraft("");
    onAssistantDraftChange("");
    verifyLiveCaptureReleased();
  }

  function switchToTextMode() {
    if (inputModeRef.current === "text" && liveStatusRef.current === "idle") return;
    setInputMode("text");
    inputModeRef.current = "text";
    stopLiveMode();
    onStatus({ tone: "idle", text: "Text input ready" });
  }

  function switchToLiveMode() {
    if (performance.now() < liveSwitchLockedUntilRef.current) return;
    setInputMode("live");
    inputModeRef.current = "live";
    if (liveStatusRef.current === "error") {
      updateLiveStatus("idle");
    }
    if (liveStatusRef.current === "idle") {
      void startLiveMode();
    }
  }

  function toggleLiveMode() {
    const liveActive = inputModeRef.current === "live" && liveStatusRef.current !== "idle" && liveStatusRef.current !== "error";
    if (liveActive) {
      switchToTextMode();
      return;
    }
    switchToLiveMode();
  }

  async function handleLiveRuntimeEvent(event: RuntimeEvent, socket: WebSocket, generation: number) {
    if (!isCurrentLiveSocket(socket, generation)) return;
    if (event.type === "live_ready") {
      if (inputModeRef.current === "live" || liveCaptureRequestedRef.current) {
        onStatus({ tone: "busy", text: "Live voice connected" });
      }
      return;
    }
    if (event.type === "live_setup_complete") {
      liveSetupAtRef.current = performance.now();
      liveSetupCompleteRef.current = true;
      if (liveCaptureRequestedRef.current) {
        await startLiveCaptureWhenReady(socket, generation);
      } else {
        scheduleLivePrewarmIdleClose();
      }
      return;
    }
    if (event.type === "error" && inputModeRef.current !== "live" && !liveCaptureRequestedRef.current) {
      liveExpectedCloseRef.current = true;
      closeLiveSocket(false);
      return;
    }
    if (event.type === "live_resumption_token") {
      const handle = liveEventDataString(event.data, "handle");
      liveResumptionHandleRef.current = handle;
      if (sessionId && typeof sessionStorage !== "undefined") {
        const key = liveResumptionStorageKey(sessionId);
        if (handle) {
          sessionStorage.setItem(key, handle);
        } else {
          sessionStorage.removeItem(key);
        }
      }
      return;
    }
    if (!isCurrentLiveSession(socket, generation)) return;
    if (event.type === "live_go_away") {
      onStatus({ tone: "busy", text: "Refreshing live voice" });
      stopLiveCapture(false);
      liveExpectedCloseRef.current = true;
      closeLiveSocket(true);
      liveExpectedCloseRef.current = false;
      liveStatusRef.current = "idle";
      setLiveStatus("idle");
      await connectLiveSocket(liveCaptureRequestedRef.current);
      return;
    }
    if (event.type === "live_transcript" && event.role === "user") {
      if (!liveFirstInputTranscriptAtRef.current) {
        liveFirstInputTranscriptAtRef.current = performance.now();
        const setupAt = liveSetupAtRef.current || liveSessionStartedAtRef.current;
        onStatus({ tone: "busy", text: `First transcript ${Math.max(0, Math.round(liveFirstInputTranscriptAtRef.current - setupAt))} ms` });
      }
      const content = event.content || "";
      if (isNoisyLiveTranscript(content)) return;
      setLiveUserDraft((current) => mergeLiveTranscript(current, content));
      return;
    }
    if (event.type === "live_transcript" && event.role === "assistant") {
      if (!liveFirstOutputTranscriptAtRef.current) {
        liveFirstOutputTranscriptAtRef.current = performance.now();
      }
      pauseLiveInput();
      onAssistantDraftChange((current) => mergeLiveTranscript(current, event.content || ""));
      return;
    }
    if (event.type === "live_response_start") {
      pauseLiveInput();
      return;
    }
    if (event.type === "live_response_end") {
      resumeLiveInputAfterPlayback(socket, generation);
      return;
    }
    if (event.type === "live_audio") {
      pauseLiveInput();
      liveAudioChunkCountRef.current += 1;
      if (!liveFirstAudioAtRef.current) {
        liveFirstAudioAtRef.current = performance.now();
        const setupAt = liveSetupAtRef.current || liveSessionStartedAtRef.current;
        onStatus({ tone: "busy", text: `First voice ${Math.max(0, Math.round(liveFirstAudioAtRef.current - setupAt))} ms` });
      }
      onStatus({ tone: "busy", text: "Playing voice" });
      await queueLiveAudio(event.data);
      return;
    }
    if (event.type === "live_interrupted") {
      liveInputPausedRef.current = false;
      clearLiveResumeCaptureTimer();
      onAssistantDraftChange("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      stopLivePlayback();
      updateLiveStatus("listening");
      onStatus({ tone: "idle", text: "Voice interrupted" });
      return;
    }
    if (event.type === "live_skill_start") {
      pauseLiveInput();
      stopLivePlayback();
      onAssistantDraftChange("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      onStatus({ tone: "busy", text: "Running skill" });
      return;
    }
    if (event.type === "live_skill_result") {
      onStatus({ tone: "ok", text: "Skill completed" });
      resumeLiveInputAfterPlayback(socket, generation);
      return;
    }
    if (event.type === "error") {
      const message = liveErrorMessage(event.error || event.content || "Live voice failed.", event.data);
      onError(message);
      onStatus({ tone: "error", text: liveErrorStatus(event.data) });
      setInputMode("text");
      inputModeRef.current = "text";
      liveExpectedCloseRef.current = true;
      liveCaptureRequestedRef.current = false;
      updateLiveStatus("error");
      cleanupLiveAudio();
      closeLiveSocket(false);
      return;
    }
    if (event.type === "done") {
      resumeLiveInputAfterPlayback(socket, generation);
      return;
    }
    if (event.type === "message" && event.role === "assistant" && isLiveSkillEvent(event)) {
      onLiveSkillMessage?.(event);
      return;
    }
    onRuntimeEvent(event);
    if (event.type === "message" && event.role === "user") {
      setLiveUserDraft("");
    }
    if (event.type === "message" && event.role === "assistant") {
      const total = liveSessionStartedAtRef.current ? Math.max(0, Math.round(performance.now() - liveSessionStartedAtRef.current)) : 0;
      onStatus(liveAudioChunkCountRef.current > 0
        ? { tone: "ok", text: total ? `Voice response played in ${total} ms` : "Voice response played" }
        : { tone: "ok", text: "Voice transcript received" });
      resumeLiveInputAfterPlayback(socket, generation);
    }
  }

  async function startLiveCaptureWhenReady(socket: WebSocket, generation: number) {
    if (!isCurrentLiveSocket(socket, generation)) return;
    if (!liveCaptureRequestedRef.current) return;
    enterLiveCaptureMode();
    try {
      await ensureLivePlaybackContext();
    } catch {
      if (!isCurrentLiveSocket(socket, generation)) return;
      onError("Audio playback is unavailable in this browser.");
      updateLiveStatus("error");
      liveCaptureRequestedRef.current = false;
      closeLiveSocket(true);
      return;
    }
    try {
      await startLiveCapture(socket, generation);
    } catch (error) {
      if (!isCurrentLiveSocket(socket, generation)) return;
      onError(errorMessage(error));
      updateLiveStatus("error");
      onStatus({ tone: "error", text: "Microphone unavailable" });
      liveCaptureRequestedRef.current = false;
      closeLiveSocket(true);
    }
  }

  async function startLiveCapture(socket: WebSocket, generation: number) {
    if (socket.readyState !== WebSocket.OPEN) throw new Error("Live voice connection is not ready.");
    if (!isCurrentLiveSession(socket, generation)) return;
    if (liveMediaRef.current) return;
    const captureGeneration = ++liveCaptureGenerationRef.current;
    const stream = await navigator.mediaDevices.getUserMedia({ audio: liveAudioConstraints });
    if (!isCurrentLiveCapture(socket, generation, captureGeneration)) {
      stopMediaStream(stream);
      return;
    }
    const track = stream.getAudioTracks()[0];
    if (!track) {
      stopMediaStream(stream);
      throw new Error("No microphone input device was found.");
    }
    const AudioContextCtor = window.AudioContext || (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioContextCtor) {
      stopMediaStream(stream);
      throw new Error("AudioContext is unavailable.");
    }
    const audioContext = new AudioContextCtor();
    const trackSettings = track.getSettings?.() || {};
    const deviceSummary = liveDeviceSummary(track, trackSettings, audioContext.sampleRate);
    socket.send(JSON.stringify({ type: "client_trace", content: JSON.stringify(deviceSummary) }));
    if (audioContext.sampleRate < 8000 || audioContext.sampleRate > 96000) {
      onStatus({ tone: "error", text: `Unexpected microphone sample rate ${Math.round(audioContext.sampleRate)} Hz` });
    }
    track.onended = () => {
      if (!isCurrentLiveCapture(socket, generation, captureGeneration)) return;
      stopLiveCapture();
      if (inputModeRef.current === "live") {
        updateLiveStatus("paused");
        onStatus({ tone: "error", text: "Microphone device disconnected" });
      }
    };
    track.onmute = () => {
      if (isCurrentLiveCapture(socket, generation, captureGeneration)) {
        onStatus({ tone: "busy", text: "Microphone is muted by the device" });
      }
    };
    track.onunmute = () => {
      if (isCurrentLiveCapture(socket, generation, captureGeneration)) {
        onStatus({ tone: "busy", text: "Microphone listening" });
      }
    };
    const source = audioContext.createMediaStreamSource(stream);
    const processor = audioContext.createScriptProcessor(liveVoiceProcessorBufferSize, 1, 1);
    let speechActive = false;
    let speechCandidateStartedAt = 0;
    let lastSpeechAt = 0;
    let calibrationFrames = 0;
    let calibrationRMS = 0;
    let noiseFloor = 0.006;
    let calibrationNoticeSent = false;
    const preSpeechFrames: Uint8Array[] = [];
    const pendingSpeechFrames: Uint8Array[] = [];
    const sendAudioFrame = (data: string) => {
      if (socket.bufferedAmount > liveSocketBackpressureBytes) return;
      liveAudioBytesSentRef.current += Math.ceil(data.length * 0.75);
      socket.send(JSON.stringify({
        type: "audio",
        mime_type: `audio/pcm;rate=${liveTargetSampleRate}`,
        data
      }));
    };
    const sendActivity = (type: "activity_start" | "activity_end") => {
      if (socket.readyState !== WebSocket.OPEN) return;
      socket.send(JSON.stringify({ type }));
    };
    processor.onaudioprocess = (event) => {
      if (!isCurrentLiveSession(socket, generation) || socket.readyState !== WebSocket.OPEN) return;
      if (liveInputPausedRef.current) {
        if (speechActive) {
          speechActive = false;
          sendActivity("activity_end");
        }
        speechCandidateStartedAt = 0;
        preSpeechFrames.length = 0;
        pendingSpeechFrames.length = 0;
        return;
      }
      const input = event.inputBuffer.getChannelData(0);
      const metrics = audioFrameMetrics(input);
      const now = performance.now();
      if (calibrationFrames < liveCalibrationFrameCount) {
        calibrationFrames += 1;
        calibrationRMS += metrics.rms;
        noiseFloor = Math.max(0.003, calibrationRMS / calibrationFrames);
        if (calibrationFrames === liveCalibrationFrameCount && !calibrationNoticeSent) {
          calibrationNoticeSent = true;
          const averageRMS = calibrationRMS / calibrationFrames;
          if (averageRMS < 0.0035) {
            onStatus({ tone: "busy", text: "Microphone input is very quiet" });
          } else if (metrics.peak > 0.94 || averageRMS > 0.32) {
            onStatus({ tone: "busy", text: "Microphone input is very loud" });
          }
        }
      } else if (!speechActive) {
        noiseFloor = noiseFloor * 0.96 + metrics.rms * 0.04;
      }
      const pcm = downsampleToPCM16(input, audioContext.sampleRate, liveTargetSampleRate);
      if (!pcm.length) return;
      const speechThreshold = Math.max(0.018, Math.min(0.09, noiseFloor * 4.8 + 0.007));
      const peakThreshold = Math.max(0.12, speechThreshold * 4.5);
      const hasSpeech = metrics.rms >= speechThreshold || metrics.peak >= peakThreshold;
      if (hasSpeech) {
        lastSpeechAt = now;
        if (!speechActive) {
          if (!speechCandidateStartedAt) {
            speechCandidateStartedAt = now;
            pendingSpeechFrames.length = 0;
          }
          pendingSpeechFrames.push(pcm);
          if (now - speechCandidateStartedAt < liveSpeechStartMinMs) {
            return;
          }
          speechActive = true;
          speechCandidateStartedAt = 0;
          sendActivity("activity_start");
          updateLiveStatus("speaking");
          onStatus({ tone: "busy", text: "Detected speech" });
          for (const frame of preSpeechFrames.splice(0)) {
            sendAudioFrame(bytesToBase64(frame));
          }
          for (const frame of pendingSpeechFrames.splice(0)) {
            sendAudioFrame(bytesToBase64(frame));
          }
          return;
        }
      }
      if (speechActive) {
        sendAudioFrame(bytesToBase64(pcm));
        if (!hasSpeech && now - lastSpeechAt > liveEndOfSpeechMs) {
          speechActive = false;
          speechCandidateStartedAt = 0;
          preSpeechFrames.length = 0;
          pendingSpeechFrames.length = 0;
          sendActivity("activity_end");
          updateLiveStatus("thinking");
          onStatus({ tone: "busy", text: "Processing voice" });
        }
        return;
      }
      speechCandidateStartedAt = 0;
      pendingSpeechFrames.length = 0;
      preSpeechFrames.push(pcm);
      if (preSpeechFrames.length > livePreSpeechFrameLimit) {
        preSpeechFrames.shift();
      }
    };
    source.connect(processor);
    processor.connect(audioContext.destination);
    if (!isCurrentLiveCapture(socket, generation, captureGeneration)) {
      processor.onaudioprocess = null;
      safeDisconnect(processor);
      safeDisconnect(source);
      stopMediaStream(stream);
      void audioContext.close();
      return;
    }
    liveMediaRef.current = stream;
    liveAudioContextRef.current = audioContext;
    liveSourceRef.current = source;
    liveProcessorRef.current = processor;
    updateLiveStatus("listening");
    onStatus({ tone: "busy", text: deviceSummary.label ? `Listening on ${deviceSummary.label}` : "Listening" });
  }

  function pauseLiveInput() {
    clearLiveResumeCaptureTimer();
    liveInputPausedRef.current = true;
    updateLiveStatus("responding");
    onStatus({ tone: "busy", text: "Processing voice" });
  }

  function resumeLiveInputAfterPlayback(socket: WebSocket, generation: number) {
    clearLiveResumeCaptureTimer();
    void livePlaybackQueueRef.current.catch(() => {}).then(() => {
      if (!isCurrentLiveSession(socket, generation)) return;
      const playbackContext = livePlaybackContextRef.current;
      const remainingMs = playbackContext
        ? Math.max(0, Math.ceil((livePlaybackTimeRef.current - playbackContext.currentTime) * 1000))
        : 0;
      liveResumeCaptureTimerRef.current = window.setTimeout(() => {
        liveResumeCaptureTimerRef.current = null;
        if (!isCurrentLiveSession(socket, generation)) return;
        liveInputPausedRef.current = false;
        if (liveCaptureRequestedRef.current && liveMediaRef.current) {
          updateLiveStatus("listening");
          onStatus({ tone: "busy", text: "Listening" });
        } else {
          scheduleLiveCaptureResume(socket, generation);
        }
      }, remainingMs + 80);
    });
  }

  function stopLiveCapture(sendEnd = true) {
    liveCaptureGenerationRef.current += 1;
    if (liveProcessorRef.current) {
      liveProcessorRef.current.onaudioprocess = null;
      safeDisconnect(liveProcessorRef.current);
    }
    if (liveSourceRef.current) {
      safeDisconnect(liveSourceRef.current);
    }
    if (liveMediaRef.current) {
      stopMediaStream(liveMediaRef.current);
    }
    void liveAudioContextRef.current?.close().catch(() => {});
    const socket = liveSocketRef.current;
    if (sendEnd && socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "audio_end" }));
    }
    liveProcessorRef.current = null;
    liveSourceRef.current = null;
    liveMediaRef.current = null;
    liveAudioContextRef.current = null;
  }

  function isCurrentLiveSession(socket: WebSocket, generation: number) {
    return isCurrentLiveSocket(socket, generation) && inputModeRef.current === "live";
  }

  function isCurrentLiveSocket(socket: WebSocket, generation: number) {
    return liveSocketRef.current === socket && liveSessionGenerationRef.current === generation;
  }

  function isCurrentLiveGeneration(generation: number) {
    return liveSessionGenerationRef.current === generation && inputModeRef.current === "live";
  }

  function isCurrentLiveCapture(socket: WebSocket, generation: number, captureGeneration: number) {
    return isCurrentLiveSession(socket, generation) && liveCaptureGenerationRef.current === captureGeneration;
  }

  function cleanupLiveAudio() {
    stopLiveCapture();
    stopLivePlayback();
    livePlaybackGainRef.current?.disconnect();
    void livePlaybackContextRef.current?.close();
    livePlaybackGainRef.current = null;
    livePlaybackContextRef.current = null;
  }

  function clearLiveReconnectTimer() {
    if (liveReconnectTimerRef.current !== null) {
      window.clearTimeout(liveReconnectTimerRef.current);
      liveReconnectTimerRef.current = null;
    }
  }

  function clearLiveResumeCaptureTimer() {
    if (liveResumeCaptureTimerRef.current !== null) {
      window.clearTimeout(liveResumeCaptureTimerRef.current);
      liveResumeCaptureTimerRef.current = null;
    }
  }

  function clearLivePrewarmTimer() {
    if (livePrewarmTimerRef.current !== null) {
      window.clearTimeout(livePrewarmTimerRef.current);
      livePrewarmTimerRef.current = null;
    }
  }

  function clearLivePrewarmIdleTimer() {
    if (livePrewarmIdleTimerRef.current !== null) {
      window.clearTimeout(livePrewarmIdleTimerRef.current);
      livePrewarmIdleTimerRef.current = null;
    }
  }

  function scheduleLivePrewarmIdleClose() {
    clearLivePrewarmIdleTimer();
    livePrewarmIdleTimerRef.current = window.setTimeout(() => {
      livePrewarmIdleTimerRef.current = null;
      if (inputModeRef.current === "live" || liveCaptureRequestedRef.current) return;
      closeLiveSocket(true);
    }, livePrewarmIdleMs);
  }

  function closeLiveSocket(sendEnd: boolean) {
    const socket = liveSocketRef.current;
    liveSocketRef.current = null;
    liveSetupCompleteRef.current = false;
    if (!socket) return;
    if (sendEnd && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "audio_end" }));
      socket.send(JSON.stringify({ type: "close" }));
    }
    socket.close();
  }

  async function scheduleLiveReconnect(generation: number) {
    if (!isCurrentLiveGeneration(generation)) return;
    if (liveReconnectAttemptsRef.current >= liveMaxReconnectAttempts) {
      updateLiveStatus("error");
      setInputMode("text");
      inputModeRef.current = "text";
      onError("Live voice disconnected. Check your network and try again.");
      onStatus({ tone: "error", text: "Live voice disconnected" });
      return;
    }
    liveReconnectAttemptsRef.current += 1;
    updateLiveStatus("reconnecting");
    onStatus({ tone: "busy", text: "Reconnecting live voice" });
    clearLiveReconnectTimer();
    liveReconnectTimerRef.current = window.setTimeout(() => {
      liveReconnectTimerRef.current = null;
      if (inputModeRef.current !== "live" || liveManualStopRef.current) return;
      liveStatusRef.current = "idle";
      setLiveStatus("idle");
      void startLiveMode();
    }, liveReconnectDelayMs);
  }

  function scheduleLiveCaptureResume(socket: WebSocket, generation: number) {
    clearLiveResumeCaptureTimer();
    if (!isCurrentLiveSession(socket, generation)) return;
    if (!liveCaptureRequestedRef.current || liveMediaRef.current) return;
    const playbackContext = livePlaybackContextRef.current;
    const playbackDelayMs = playbackContext
      ? Math.max(0, Math.ceil((livePlaybackTimeRef.current - playbackContext.currentTime) * 1000))
      : 0;
    liveResumeCaptureTimerRef.current = window.setTimeout(() => {
      liveResumeCaptureTimerRef.current = null;
      if (!isCurrentLiveSession(socket, generation)) return;
      if (!liveCaptureRequestedRef.current || liveMediaRef.current || liveStatusRef.current === "speaking") return;
      liveInputPausedRef.current = false;
      void startLiveCapture(socket, generation).catch((error) => {
        if (!isCurrentLiveSession(socket, generation)) return;
        onError(errorMessage(error));
        updateLiveStatus("error");
        onStatus({ tone: "error", text: "Microphone unavailable" });
      });
    }, playbackDelayMs + 80);
  }

  function verifyLiveCaptureReleased() {
    window.setTimeout(() => {
      const stream = liveMediaRef.current;
      if (!stream) return;
      stopMediaStream(stream);
      liveMediaRef.current = null;
    }, 250);
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
    if (!livePlaybackGainRef.current) {
      const gain = context.createGain();
      gain.gain.value = 1;
      gain.connect(context.destination);
      livePlaybackGainRef.current = gain;
    }
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
    source.connect(livePlaybackGainRef.current || context.destination);
    livePlaybackSourcesRef.current.add(source);
    source.onended = () => {
      livePlaybackSourcesRef.current.delete(source);
    };
    const startAt = Math.max(context.currentTime + 0.02, livePlaybackTimeRef.current || 0);
    source.start(startAt);
    livePlaybackTimeRef.current = startAt + buffer.duration;
  }

  return {
    liveStatus,
    liveUserDraft,
    startLiveMode,
    prewarmLiveMode,
    stopLiveMode,
    switchToTextMode,
    switchToLiveMode,
    toggleLiveMode
  };
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

function audioFrameMetrics(input: Float32Array): { rms: number; peak: number } {
  if (!input.length) return { rms: 0, peak: 0 };
  let sum = 0;
  let peak = 0;
  for (let index = 0; index < input.length; index += 1) {
    const value = Math.abs(input[index] || 0);
    sum += value * value;
    if (value > peak) peak = value;
  }
  return { rms: Math.sqrt(sum / input.length), peak };
}

async function microphonePermissionState(): Promise<PermissionState | "unknown"> {
  const permissions = navigator.permissions as (Permissions & { query?: (descriptor: PermissionDescriptor | { name: "microphone" }) => Promise<PermissionStatus> }) | undefined;
  if (!permissions?.query) return "unknown";
  try {
    const status = await permissions.query({ name: "microphone" });
    return status.state;
  } catch {
    return "unknown";
  }
}

function liveDeviceSummary(track: MediaStreamTrack, settings: MediaTrackSettings, contextSampleRate: number) {
  return {
    kind: "input_device",
    label: track.label || "",
    device_id: settings.deviceId || "",
    group_id: settings.groupId || "",
    channel_count: settings.channelCount || 0,
    sample_rate: settings.sampleRate || contextSampleRate || 0,
    context_sample_rate: contextSampleRate || 0,
    echo_cancellation: settings.echoCancellation ?? true,
    noise_suppression: settings.noiseSuppression ?? true,
    auto_gain_control: settings.autoGainControl ?? true
  };
}

function appendLiveTranscript(current: string, next: string): string {
  const text = next.trim();
  if (!text) return current;
  if (!current) return text;
  if (/^[，。！？,.!?;；:：]/.test(text)) return `${current}${text}`;
  return `${current}${/[\s\n]$/.test(current) ? "" : " "}${text}`;
}

function mergeLiveTranscript(current: string, next: string): string {
  const currentText = current.trim();
  const nextText = next.trim();
  if (!currentText) return nextText;
  if (!nextText) return currentText;
  if (nextText.startsWith(currentText)) return nextText;
  if (currentText.startsWith(nextText)) return currentText;
  return appendLiveTranscript(currentText, nextText);
}

function liveResumptionStorageKey(sessionId: string): string {
  return `agentapi.live.resume.${sessionId}`;
}

function liveEventDataString(data: unknown, key: string): string | null {
  if (!data || typeof data !== "object" || Array.isArray(data)) return null;
  const value = (data as Record<string, unknown>)[key];
  return typeof value === "string" && value.trim() ? value : null;
}

function isNoisyLiveTranscript(text: string): boolean {
  const compact = compactLiveTranscriptNoiseText(text);
  const runes = Array.from(compact);
  if (liveTranscriptNoise.meaningfulShortUtterances.includes(compact)) return false;
  if (!compact || runes.length < liveTranscriptNoise.minMeaningfulRunes) return true;
  if (liveTranscriptNoise.standaloneFillers.includes(compact)) return true;
  if (liveTranscriptNoise.repeatableFillers.some((filler) => isExtendedLiveTranscriptFiller(compact, filler))) return true;
  if (runes.length >= liveTranscriptNoise.repeatedSingleRuneMinRunes && runes.every((rune) => rune === runes[0])) return true;
  if (liveTranscriptNoise.shortContains.some((item) => runes.length <= item.maxRunes && compact.includes(item.value))) return true;
  if (isLikelyShortNonChineseNoise(compact, runes.length)) return true;
  return false;
}

function compactLiveTranscriptNoiseText(text: string): string {
  return text.trim().toLowerCase().replace(/[\s\p{P}\p{S}]+/gu, "");
}

function isLikelyShortNonChineseNoise(compact: string, runeCount: number): boolean {
  if (runeCount === 0 || runeCount > 12) return false;
  if (/[\u3400-\u9fff]/u.test(compact)) return false;
  if (/[\u3040-\u30ff\uac00-\ud7af]/u.test(compact)) return true;
  return false;
}

function isExtendedLiveTranscriptFiller(compact: string, filler: string): boolean {
  if (!compact || !filler) return false;
  if (compact === filler) return true;
  const fillerRunes = Array.from(filler);
  const lastRune = fillerRunes[fillerRunes.length - 1] || "";
  if (compact.startsWith(filler)) {
    const suffix = compact.slice(filler.length);
    if (suffix && Array.from(suffix).every((rune) => rune === lastRune)) return true;
  }
  if (compact.length > filler.length && compact.split(filler).join("") === "") return true;
  return false;
}

function stopMediaStream(stream: MediaStream) {
  stream.getTracks().forEach((track) => {
    track.enabled = false;
    track.stop();
  });
}

function safeDisconnect(node: AudioNode) {
  try {
    node.disconnect();
  } catch {
    // Already disconnected.
  }
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

function isLiveSkillEvent(event: RuntimeEvent): boolean {
  const data = event.data as { source?: unknown } | undefined;
  return data?.source === "live_skill";
}

function errorMessage(error: unknown): string {
  return error instanceof ApiError && error.requestId
    ? `${error.message} (${error.requestId})`
    : error instanceof Error
      ? userFacingErrorMessage(error.message)
      : userFacingErrorMessage(String(error));
}

function liveErrorMessage(message: string, data?: unknown): string {
  const payload = liveErrorPayload(data);
  if (payload.message) {
    return payload.message;
  }
  const text = message.trim();
  if (/live vertex access token is required|GOOGLE_APPLICATION_CREDENTIALS|VERTEX_ACCESS_TOKEN|vertex-service-account/i.test(text)) {
    return "Live mode is not configured for this environment. Ask an administrator to finish voice setup.";
  }
  return userFacingErrorMessage(text || "Live voice failed.");
}

function liveErrorStatus(data?: unknown): string {
  const { code } = liveErrorPayload(data);
  if (code === "live_credentials_missing" || code === "live_project_missing") return "Live setup required";
  if (code === "live_timeout") return "Live voice timed out";
  if (code === "live_provider_rate_limited") return "Live provider quota reached";
  if (code === "live_provider_connection") return "Live provider unavailable";
  if (code === "live_audio_invalid" || code === "live_client_protocol") return "Live audio failed";
  return "Live voice failed";
}

function liveErrorPayload(data?: unknown): { code?: string; message?: string } {
  if (!data) return {};
  if (typeof data === "string") {
    try {
      const parsed = JSON.parse(data) as { code?: unknown; message?: unknown };
      return {
        code: typeof parsed.code === "string" ? parsed.code : undefined,
        message: typeof parsed.message === "string" ? parsed.message : undefined
      };
    } catch {
      return {};
    }
  }
  const payload = data as { code?: unknown; message?: unknown };
  return {
    code: typeof payload.code === "string" ? payload.code : undefined,
    message: typeof payload.message === "string" ? payload.message : undefined
  };
}
