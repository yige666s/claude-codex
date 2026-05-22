import { Dispatch, SetStateAction, useEffect, useRef, useState } from "react";
import { ApiClient, ApiError } from "../../../api/client";
import type { RuntimeEvent } from "../../../types";
import type { Status } from "../workspaceTypes";

export type InputMode = "text" | "live";
export type LiveStatus = "idle" | "connecting" | "listening" | "paused" | "error";

type UseLiveVoiceOptions = {
  api: ApiClient;
  sessionId: string;
  onRuntimeEvent: (event: RuntimeEvent) => void;
  onAssistantDraftChange: Dispatch<SetStateAction<string>>;
  onLiveSkillMessage?: (event: RuntimeEvent) => void;
  onError: (message: string) => void;
  onStatus: (status: Status | ((current: Status) => Status)) => void;
};

const liveVoiceStartRmsThreshold = 0.012;
const liveVoiceContinueRmsThreshold = 0.007;
const liveVoiceStartFrames = 1;
const liveVoiceHangoverFrames = 9;
const liveVoicePrerollFrames = 4;
const liveVoiceProcessorBufferSize = 1024;

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
  const [liveMuted, setLiveMuted] = useState(false);
  const [speakerVolume, setSpeakerVolumeState] = useState(1);
  const [micVolume, setMicVolumeState] = useState(1);
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
  const liveMutedRef = useRef(liveMuted);
  const liveSpeakerVolumeRef = useRef(speakerVolume);
  const liveMicVolumeRef = useRef(micVolume);
  const liveStatusRef = useRef(liveStatus);
  const inputModeRef = useRef(inputMode);
  const liveSessionGenerationRef = useRef(0);
  const lastLiveSpeakerVolumeRef = useRef(1);
  const lastLiveMicVolumeRef = useRef(1);
  const liveAudioChunkCountRef = useRef(0);
  const livePlaybackQueueRef = useRef(Promise.resolve());

  useEffect(() => {
    inputModeRef.current = inputMode;
  }, [inputMode]);

  useEffect(() => {
    liveStatusRef.current = liveStatus;
  }, [liveStatus]);

  useEffect(() => {
    liveMutedRef.current = liveMuted;
    if (liveMuted) {
      stopLivePlayback();
    }
  }, [liveMuted]);

  useEffect(() => {
    liveSpeakerVolumeRef.current = liveMuted ? 0 : speakerVolume;
    if (speakerVolume > 0) {
      lastLiveSpeakerVolumeRef.current = speakerVolume;
    }
    if (livePlaybackGainRef.current) {
      livePlaybackGainRef.current.gain.value = liveMuted ? 0 : speakerVolume;
    }
  }, [liveMuted, speakerVolume]);

  useEffect(() => {
    liveMicVolumeRef.current = micVolume;
    if (micVolume > 0) {
      lastLiveMicVolumeRef.current = micVolume;
      return;
    }
    if (liveMediaRef.current) {
      stopLiveCapture();
    }
  }, [micVolume]);

  async function startLiveMode() {
    if (!sessionId || liveStatus !== "idle") return;
    if (typeof WebSocket === "undefined" || !navigator.mediaDevices?.getUserMedia) {
      onError("Live voice is unavailable in this browser.");
      setLiveStatus("error");
      return;
    }
    stopLiveMode(false);
    const generation = ++liveSessionGenerationRef.current;
    setInputMode("live");
    inputModeRef.current = "live";
    onError("");
    onAssistantDraftChange("");
    setLiveUserDraft("");
    setLiveMuted(false);
    liveAudioChunkCountRef.current = 0;
    stopLivePlayback();
    try {
      await ensureLivePlaybackContext();
    } catch {
      onError("Audio playback is unavailable in this browser.");
      setLiveStatus("error");
      return;
    }
    setLiveStatus("connecting");
    onStatus({ tone: "busy", text: "Connecting live voice" });
    const socket = new WebSocket(api.liveSessionURL(sessionId));
    liveSocketRef.current = socket;
    socket.onmessage = (message) => {
      if (!isCurrentLiveSession(socket, generation)) return;
      try {
        void handleLiveRuntimeEvent(JSON.parse(message.data) as RuntimeEvent, socket, generation);
      } catch {
        // Ignore malformed live frames; the socket error handler covers transport failures.
      }
    };
    socket.onerror = () => {
      if (!isCurrentLiveSession(socket, generation)) return;
      setLiveStatus("error");
      onStatus({ tone: "error", text: "Live voice failed" });
    };
    socket.onclose = () => {
      if (!isCurrentLiveSession(socket, generation)) return;
      cleanupLiveAudio();
      liveSocketRef.current = null;
      setLiveStatus("idle");
      onStatus((current) => current.tone === "error" ? current : { tone: "idle", text: "Live voice stopped" });
    };
  }

  function stopLiveMode(sendEnd = true) {
    liveSessionGenerationRef.current += 1;
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
    liveStatusRef.current = "idle";
    setLiveStatus("idle");
    setLiveUserDraft("");
    onAssistantDraftChange("");
  }

  function switchToTextMode() {
    if (inputMode === "text" && liveStatus === "idle") return;
    setInputMode("text");
    inputModeRef.current = "text";
    stopLiveMode();
    onStatus({ tone: "idle", text: "Text input ready" });
  }

  function switchToLiveMode() {
    setInputMode("live");
    inputModeRef.current = "live";
    if (liveStatus === "idle") {
      void startLiveMode();
    }
  }

  function toggleSpeakerMute() {
    if (inputMode !== "live" || liveStatus === "idle") return;
    setLiveMuted((current) => {
      if (current && speakerVolume <= 0) {
        setSpeakerVolumeState(lastLiveSpeakerVolumeRef.current || 1);
      }
      return !current;
    });
  }

  function setSpeakerVolume(value: number) {
    const next = clamp01(value);
    setSpeakerVolumeState(next);
    setLiveMuted(next <= 0);
  }

  async function setMicVolume(value: number) {
    const next = clamp01(value);
    updateMicVolume(next);
    if (inputMode !== "live" || liveStatus === "connecting" || liveStatus === "error") return;
    if (next <= 0) {
      stopLiveCapture();
      if (liveStatus !== "idle") setLiveStatus("paused");
      onStatus({ tone: "idle", text: "Microphone muted" });
      return;
    }
    if (liveStatus !== "listening") {
      await toggleMicMute();
    }
  }

  async function toggleMicMute() {
    if (inputMode !== "live" || liveStatus === "connecting") return;
    const socket = liveSocketRef.current;
    if (liveStatus === "listening") {
      updateMicVolume(0);
      stopLiveCapture();
      setLiveStatus("paused");
      onStatus({ tone: "idle", text: "Microphone muted" });
      return;
    }
    if (micVolume <= 0) {
      updateMicVolume(lastLiveMicVolumeRef.current || 1);
    }
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      if (liveStatus === "idle") {
        await startLiveMode();
      }
      return;
    }
    try {
      await startLiveCapture(socket, liveSessionGenerationRef.current);
    } catch (error) {
      onError(errorMessage(error));
      setLiveStatus("error");
      onStatus({ tone: "error", text: "Microphone unavailable" });
    }
  }

  async function handleLiveRuntimeEvent(event: RuntimeEvent, socket: WebSocket, generation: number) {
    if (!isCurrentLiveSession(socket, generation)) return;
    if (event.type === "live_ready") {
      onStatus({ tone: "busy", text: "Live voice connected" });
      return;
    }
    if (event.type === "live_setup_complete") {
      try {
        await startLiveCapture(socket, generation);
      } catch (error) {
        if (!isCurrentLiveSession(socket, generation)) return;
        onError(errorMessage(error));
        setLiveStatus("error");
        onStatus({ tone: "error", text: "Microphone unavailable" });
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
      onAssistantDraftChange((current) => current + (event.content || ""));
      return;
    }
    if (event.type === "live_audio") {
      liveAudioChunkCountRef.current += 1;
      if (liveMutedRef.current) {
        onStatus({ tone: "busy", text: "Voice muted" });
      } else {
        onStatus({ tone: "busy", text: "Playing voice" });
        await queueLiveAudio(event.data);
      }
      return;
    }
    if (event.type === "live_interrupted") {
      onAssistantDraftChange("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      stopLivePlayback();
      onStatus({ tone: "idle", text: "Voice interrupted" });
      return;
    }
    if (event.type === "live_skill_start") {
      stopLivePlayback();
      onAssistantDraftChange("");
      setLiveUserDraft("");
      liveAudioChunkCountRef.current = 0;
      onStatus({ tone: "busy", text: "Running skill" });
      return;
    }
    if (event.type === "live_skill_result") {
      onStatus({ tone: "ok", text: "Skill completed" });
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
      onStatus(liveMutedRef.current
        ? { tone: "ok", text: "Voice response muted" }
        : liveAudioChunkCountRef.current > 0
          ? { tone: "ok", text: "Voice response played" }
          : { tone: "ok", text: "Voice transcript received" });
    }
  }

  async function startLiveCapture(socket: WebSocket, generation: number) {
    if (socket.readyState !== WebSocket.OPEN) throw new Error("Live voice connection is not ready.");
    if (!isCurrentLiveSession(socket, generation)) return;
    if (liveMicVolumeRef.current <= 0) {
      stopLiveCapture();
      setLiveStatus("paused");
      onStatus({ tone: "idle", text: "Microphone muted" });
      return;
    }
    if (liveMediaRef.current) return;
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: false }
    });
    if (!isCurrentLiveSession(socket, generation) || liveMicVolumeRef.current <= 0) {
      stopMediaStream(stream);
      return;
    }
    const AudioContextCtor = window.AudioContext || (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioContextCtor) {
      stopMediaStream(stream);
      throw new Error("AudioContext is unavailable.");
    }
    const audioContext = new AudioContextCtor();
    const source = audioContext.createMediaStreamSource(stream);
    const processor = audioContext.createScriptProcessor(liveVoiceProcessorBufferSize, 1, 1);
    let activeSpeech = false;
    let voicedFrames = 0;
    let quietFrames = 0;
    const prerollFrames: string[] = [];
    const sendAudioFrame = (data: string) => {
      socket.send(JSON.stringify({
        type: "audio",
        mime_type: "audio/pcm;rate=16000",
        data
      }));
    };
    processor.onaudioprocess = (event) => {
      if (!isCurrentLiveSession(socket, generation) || socket.readyState !== WebSocket.OPEN) return;
      const input = event.inputBuffer.getChannelData(0);
      const currentMicVolume = liveMicVolumeRef.current;
      if (currentMicVolume <= 0) return;
      const adjustedInput = scaleAudio(input, currentMicVolume);
      const rms = audioRMS(adjustedInput);
      const pcm = downsampleToPCM16(adjustedInput, audioContext.sampleRate, 16000);
      if (!pcm.length) return;
      const frame = bytesToBase64(pcm);
      if (activeSpeech) {
        sendAudioFrame(frame);
        quietFrames = rms >= liveVoiceContinueRmsThreshold ? 0 : quietFrames + 1;
        if (quietFrames >= liveVoiceHangoverFrames) {
          activeSpeech = false;
          voicedFrames = 0;
          quietFrames = 0;
        }
        return;
      }
      prerollFrames.push(frame);
      if (prerollFrames.length > liveVoicePrerollFrames) {
        prerollFrames.shift();
      }
      voicedFrames = rms >= liveVoiceStartRmsThreshold ? voicedFrames + 1 : 0;
      if (voicedFrames < liveVoiceStartFrames) return;
      activeSpeech = true;
      quietFrames = 0;
      for (const bufferedFrame of prerollFrames) {
        sendAudioFrame(bufferedFrame);
      }
      prerollFrames.length = 0;
    };
    source.connect(processor);
    processor.connect(audioContext.destination);
    liveMediaRef.current = stream;
    liveAudioContextRef.current = audioContext;
    liveSourceRef.current = source;
    liveProcessorRef.current = processor;
    liveStatusRef.current = "listening";
    setLiveStatus("listening");
    onStatus({ tone: "busy", text: "Listening" });
  }

  function stopLiveCapture() {
    liveProcessorRef.current?.disconnect();
    liveSourceRef.current?.disconnect();
    liveMediaRef.current?.getTracks().forEach((track) => track.stop());
    void liveAudioContextRef.current?.close();
    const socket = liveSocketRef.current;
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "audio_end" }));
    }
    liveProcessorRef.current = null;
    liveSourceRef.current = null;
    liveMediaRef.current = null;
    liveAudioContextRef.current = null;
  }

  function isCurrentLiveSession(socket: WebSocket, generation: number) {
    return liveSocketRef.current === socket && liveSessionGenerationRef.current === generation && inputModeRef.current === "live";
  }

  function updateMicVolume(next: number) {
    liveMicVolumeRef.current = next;
    if (next > 0) {
      lastLiveMicVolumeRef.current = next;
    }
    setMicVolumeState(next);
  }

  function cleanupLiveAudio() {
    stopLiveCapture();
    stopLivePlayback();
    livePlaybackGainRef.current?.disconnect();
    void livePlaybackContextRef.current?.close();
    livePlaybackGainRef.current = null;
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
    if (!livePlaybackGainRef.current) {
      const gain = context.createGain();
      gain.gain.value = liveMutedRef.current ? 0 : liveSpeakerVolumeRef.current;
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
    inputMode,
    liveStatus,
    liveMuted,
    speakerVolume,
    micVolume,
    liveUserDraft,
    startLiveMode,
    stopLiveMode,
    switchToTextMode,
    switchToLiveMode,
    toggleSpeakerMute,
    toggleMicMute,
    setSpeakerVolume,
    setMicVolume
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

function audioRMS(input: Float32Array): number {
  if (!input.length) return 0;
  let sum = 0;
  for (let index = 0; index < input.length; index += 1) {
    const sample = input[index] || 0;
    sum += sample * sample;
  }
  return Math.sqrt(sum / input.length);
}

function scaleAudio(input: Float32Array, volume: number): Float32Array {
  const scale = clamp01(volume);
  if (scale >= 0.999) return input;
  const output = new Float32Array(input.length);
  for (let index = 0; index < input.length; index += 1) {
    output[index] = (input[index] || 0) * scale;
  }
  return output;
}

function clamp01(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(1, value));
}

function stopMediaStream(stream: MediaStream) {
  stream.getTracks().forEach((track) => track.stop());
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
      ? error.message
      : String(error);
}
