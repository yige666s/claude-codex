import { KeyboardEvent, Ref, RefObject } from "react";
import { AlertCircle, FileUp, MessageCircle, Mic, MicOff, Send, Square, Volume2, VolumeX, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { Textarea } from "../../../components/ui/textarea";
import type { Asset } from "../../../types";

type InputMode = "text" | "live";
type LiveStatus = "idle" | "connecting" | "listening" | "paused" | "error";

type MessageComposerProps = {
  runtimeError: string;
  uploadError: string;
  responseTiming: { ttftMs?: number; totalMs?: number } | null;
  pendingAttachments: Asset[];
  attachmentInputRef: RefObject<HTMLInputElement | null>;
  composerInputRef: RefObject<HTMLTextAreaElement | null>;
  uploading: boolean;
  inputMode: InputMode;
  liveStatus: LiveStatus;
  liveMuted: boolean;
  busyChat: boolean;
  sessionId: string;
  draft: string;
  onClearRuntimeError: () => void;
  onClearUploadError: () => void;
  onRemovePendingAttachment: (id: string) => void;
  onUploadAttachment: (files: FileList | null) => void;
  onDraftChange: (value: string) => void;
  onSendMessage: () => void;
  onCancelChat: () => void;
  onSwitchToText: () => void;
  onSwitchToLive: () => void;
  onToggleLiveMute: () => void;
  onToggleLiveCapture: () => void;
  formatNumber: (value: number) => string;
};

const acceptedAttachmentTypes = ".png,.jpg,.jpeg,.jfif,.webp,.gif,.avif,.bmp,.tif,.tiff,.heic,.heif,.pdf,.txt,.md,.csv,.json,.docx,.xlsx,.pptx,image/png,image/jpeg,image/pjpeg,image/webp,image/gif,image/avif,image/bmp,image/tiff,image/heic,image/heif,application/pdf,text/plain,text/markdown,text/csv,application/json";

export function MessageComposer({
  runtimeError,
  uploadError,
  responseTiming,
  pendingAttachments,
  attachmentInputRef,
  composerInputRef,
  uploading,
  inputMode,
  liveStatus,
  liveMuted,
  busyChat,
  sessionId,
  draft,
  onClearRuntimeError,
  onClearUploadError,
  onRemovePendingAttachment,
  onUploadAttachment,
  onDraftChange,
  onSendMessage,
  onCancelChat,
  onSwitchToText,
  onSwitchToLive,
  onToggleLiveMute,
  onToggleLiveCapture,
  formatNumber
}: MessageComposerProps) {
  const canUseText = inputMode === "text" && liveStatus === "idle";
  const canSend = canUseText && (!!draft.trim() || pendingAttachments.length > 0) && !!sessionId;

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      onSendMessage();
    }
  };

  return (
    <footer className="composer">
      {runtimeError && (
        <div className="composer-error" role="alert">
          <AlertCircle size={16} />
          <span>{runtimeError}</span>
          <Button className="icon ghost" onClick={onClearRuntimeError} title="Dismiss error" aria-label="Dismiss error"><X size={14} /></Button>
        </div>
      )}
      {uploadError && (
        <div className="composer-error upload-error" role="alert">
          <span>{uploadError}</span>
          <Button className="icon ghost" onClick={onClearUploadError} title="Dismiss upload error" aria-label="Dismiss upload error"><X size={14} /></Button>
        </div>
      )}
      {responseTiming && (
        <div className="response-metrics" aria-live="polite">
          <span>TTFT {formatNumber(responseTiming.ttftMs || 0)} ms</span>
          {responseTiming.totalMs !== undefined && <span>Total {formatNumber(responseTiming.totalMs)} ms</span>}
        </div>
      )}
      {pendingAttachments.length > 0 && (
        <div className="pending-attachments" aria-label="Pending attachments">
          {pendingAttachments.map((asset) => (
            <span className="pending-attachment" key={asset.id} title={asset.filename}>
              <FileUp size={13} />
              <span>{asset.filename}</span>
              <Button
                className="icon ghost"
                onClick={() => onRemovePendingAttachment(asset.id)}
                title={`Remove ${asset.filename}`}
                aria-label={`Remove ${asset.filename}`}
              >
                <X size={12} />
              </Button>
            </span>
          ))}
        </div>
      )}
      <div className="composer-row">
        <Button
          type="button"
          className="composer-upload"
          title="Upload attachment"
          aria-label="Upload attachment"
          onClick={() => attachmentInputRef.current?.click()}
          disabled={uploading || !canUseText}
        >
          <FileUp size={18} />
        </Button>
        <Input
          ref={attachmentInputRef as Ref<HTMLInputElement>}
          className="composer-file-input"
          type="file"
          tabIndex={-1}
          aria-hidden="true"
          accept={acceptedAttachmentTypes}
          onChange={(event) => onUploadAttachment(event.currentTarget.files)}
          disabled={uploading || !canUseText}
        />
        <Textarea
          ref={composerInputRef as Ref<HTMLTextAreaElement>}
          value={draft}
          aria-label="Message"
          placeholder={inputMode === "live" ? "Live mode is active" : "输入消息，或用 /skills 调用工作流"}
          onChange={(event) => onDraftChange(event.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!canUseText}
          rows={1}
        />
        <div className="composer-actions">
          <div className="input-mode-toggle" role="group" aria-label="Input mode">
            <Button
              type="button"
              className={inputMode === "text" ? "active" : ""}
              onClick={onSwitchToText}
              disabled={busyChat}
              title="Text mode"
              aria-label="Text mode"
            >
              <MessageCircle size={16} />
              <span>Text</span>
            </Button>
            <Button
              type="button"
              className={inputMode === "live" ? "active" : ""}
              onClick={onSwitchToLive}
              disabled={!sessionId || busyChat}
              title="Live mode"
              aria-label="Live mode"
            >
              <Mic size={16} />
              <span>Live</span>
            </Button>
          </div>
          {inputMode === "live" && (
            <>
              <Button
                type="button"
                className={`voice-output-toggle ${liveMuted ? "muted" : ""}`}
                onClick={onToggleLiveMute}
                disabled={liveStatus === "idle" || liveStatus === "connecting" || !sessionId}
                title={liveMuted ? "Unmute voice output" : "Mute voice output"}
                aria-label={liveMuted ? "Unmute voice output" : "Mute voice output"}
                aria-pressed={liveStatus !== "idle" && !liveMuted}
              >
                {liveMuted ? <VolumeX size={18} /> : <Volume2 size={18} />}
              </Button>
              <Button
                type="button"
                className={`live-control ${liveStatus === "listening" ? "active" : ""}`}
                onClick={onToggleLiveCapture}
                disabled={!sessionId || busyChat || liveStatus === "connecting" || liveStatus === "error"}
                title={liveStatus === "listening" ? "Pause microphone" : "Resume microphone"}
                aria-label={liveStatus === "listening" ? "Pause microphone" : "Resume microphone"}
                aria-pressed={liveStatus === "listening"}
              >
                {liveStatus === "listening" ? <Mic size={18} /> : <MicOff size={18} />}
              </Button>
            </>
          )}
          {busyChat ? (
            <Button className="stop-generation" onClick={onCancelChat} title="Stop generation" aria-label="Stop generation">
              <span><Square size={16} fill="currentColor" /></span>
            </Button>
          ) : (
            <Button className="primary send" onClick={onSendMessage} disabled={!canSend} title="Send" aria-label="Send">
              <Send size={21} />
            </Button>
          )}
        </div>
      </div>
    </footer>
  );
}
