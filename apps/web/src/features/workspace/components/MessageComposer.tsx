import { KeyboardEvent, Ref, RefObject } from "react";
import { Textarea } from "../../../components/ui/textarea";
import type { Asset } from "../../../types";
import type { InputMode, LiveStatus } from "../hooks/useLiveVoice";
import type { ComposerToolID } from "../workspaceTypes";
import { ComposerToolChips } from "./composer/ComposerToolChips";
import { ComposerUploadButton } from "./composer/ComposerUploadButton";
import { LiveVoiceControls } from "./composer/LiveVoiceControls";
import { PendingAttachments } from "./composer/PendingAttachments";
import { ResponseTimingBadges } from "./composer/ResponseTimingBadges";
import { RuntimeErrorBanner } from "./composer/RuntimeErrorBanner";
import { SendButton } from "./composer/SendButton";
import { ToolModeSelector } from "./composer/ToolModeSelector";

type MessageComposerProps = {
  runtimeError: string;
  uploadError: string;
  responseTiming: { ttftMs?: number; totalMs?: number } | null;
  pendingAttachments: Asset[];
  selectedToolId: ComposerToolID | "";
  showToolChips: boolean;
  attachmentInputRef: RefObject<HTMLInputElement | null>;
  composerInputRef: RefObject<HTMLTextAreaElement | null>;
  uploading: boolean;
  inputMode: InputMode;
  liveStatus: LiveStatus;
  liveMuted: boolean;
  liveSpeakerVolume: number;
  liveMicVolume: number;
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
  onSelectChatMode: () => void;
  onSwitchToLive: () => void;
  onSelectTool: (toolId: ComposerToolID) => void;
  onToggleLiveMute: () => void;
  onToggleLiveCapture: () => void;
  onLiveSpeakerVolumeChange: (value: number) => void;
  onLiveMicVolumeChange: (value: number) => void;
  formatNumber: (value: number) => string;
};

const acceptedAttachmentTypes = ".png,.jpg,.jpeg,.jfif,.webp,.gif,.avif,.bmp,.tif,.tiff,.heic,.heif,.pdf,.txt,.md,.csv,.json,.docx,.xlsx,.pptx,image/png,image/jpeg,image/pjpeg,image/webp,image/gif,image/avif,image/bmp,image/tiff,image/heic,image/heif,application/pdf,text/plain,text/markdown,text/csv,application/json";

export function MessageComposer({
  runtimeError,
  uploadError,
  responseTiming,
  pendingAttachments,
  selectedToolId,
  showToolChips,
  attachmentInputRef,
  composerInputRef,
  uploading,
  inputMode,
  liveStatus,
  liveMuted,
  liveSpeakerVolume,
  liveMicVolume,
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
  onSelectChatMode,
  onSwitchToLive,
  onSelectTool,
  onToggleLiveMute,
  onToggleLiveCapture,
  onLiveSpeakerVolumeChange,
  onLiveMicVolumeChange,
  formatNumber
}: MessageComposerProps) {
  const canUseText = inputMode === "text" && liveStatus === "idle";
  const canSend = canUseText && (!!draft.trim() || pendingAttachments.length > 0) && !!sessionId;
  const expandedComposer = draft.length > 80 || draft.includes("\n");

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      onSendMessage();
    }
  };

  return (
    <footer className={`composer ${expandedComposer ? "composer-expanded" : ""}`}>
      <RuntimeErrorBanner message={runtimeError} onDismiss={onClearRuntimeError} />
      <RuntimeErrorBanner message={uploadError} upload onDismiss={onClearUploadError} />
      <ResponseTimingBadges timing={responseTiming} formatNumber={formatNumber} />
      <PendingAttachments attachments={pendingAttachments} onRemove={onRemovePendingAttachment} />
      <div className="composer-row">
        <ComposerUploadButton
          inputRef={attachmentInputRef}
          uploading={uploading}
          disabled={!canUseText}
          accept={acceptedAttachmentTypes}
          onUpload={onUploadAttachment}
        />
        <Textarea
          ref={composerInputRef as Ref<HTMLTextAreaElement>}
          value={draft}
          aria-label="Message"
          placeholder={inputMode === "live" ? "Live mode is active" : placeholderForTool(selectedToolId)}
          onChange={(event) => onDraftChange(event.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!canUseText}
          rows={1}
        />
        <div className="composer-actions">
          <ToolModeSelector
            inputMode={inputMode}
            busyChat={busyChat}
            sessionId={sessionId}
            onSelectChat={onSelectChatMode}
            onSelectLive={onSwitchToLive}
          />
          <LiveVoiceControls
            inputMode={inputMode}
            liveStatus={liveStatus}
            liveMuted={liveMuted}
            speakerVolume={liveSpeakerVolume}
            micVolume={liveMicVolume}
            busyChat={busyChat}
            sessionId={sessionId}
            onToggleSpeakerMute={onToggleLiveMute}
            onToggleMicMute={onToggleLiveCapture}
            onSpeakerVolumeChange={onLiveSpeakerVolumeChange}
            onMicVolumeChange={onLiveMicVolumeChange}
          />
          <SendButton busyChat={busyChat} canSend={canSend} onSend={onSendMessage} onCancel={onCancelChat} />
        </div>
      </div>
      {showToolChips && <ComposerToolChips selectedToolId={selectedToolId} disabled={!canUseText || busyChat} onSelectTool={onSelectTool} />}
    </footer>
  );
}

function placeholderForTool(toolId: ComposerToolID | ""): string {
  if (toolId === "image") return "Describe the image you want to generate...";
  if (toolId === "web-search") return "Ask what you want to look up...";
  if (toolId === "thinking") return "Ask a question that needs deeper reasoning...";
  return "Initiate a query or send a command to the AI...";
}
