import { KeyboardEvent, Ref, RefObject } from "react";
import { Textarea } from "../../../components/ui/textarea";
import type { Asset } from "../../../types";
import type { LiveStatus } from "../hooks/useLiveVoice";
import type { ComposerToolID } from "../workspaceTypes";
import { ComposerToolChips } from "./composer/ComposerToolChips";
import { ComposerUploadButton } from "./composer/ComposerUploadButton";
import { LiveVoiceControls } from "./composer/LiveVoiceControls";
import { PendingAttachments } from "./composer/PendingAttachments";
import { ResponseTimingBadges } from "./composer/ResponseTimingBadges";
import { RuntimeErrorBanner } from "./composer/RuntimeErrorBanner";
import { SendButton } from "./composer/SendButton";

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
  liveStatus: LiveStatus;
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
  onSelectTool: (toolId: ComposerToolID) => void;
  onToggleLive: () => void;
  onPrewarmLive: () => void;
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
  liveStatus,
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
  onSelectTool,
  onToggleLive,
  onPrewarmLive,
  formatNumber
}: MessageComposerProps) {
  const canUseText = true;
  const canSend = canUseText && (!!draft.trim() || pendingAttachments.length > 0) && !!sessionId;
  const expandedComposer = draft.length > 80 || draft.includes("\n");
  const composerClassName = [
    "composer",
    expandedComposer ? "composer-expanded" : "",
    showToolChips ? "composer-with-tools" : ""
  ].filter(Boolean).join(" ");

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      onSendMessage();
    }
  };

  return (
    <footer className={composerClassName}>
      <div className="composer-surface">
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
            placeholder={placeholderForTool(selectedToolId)}
            onChange={(event) => onDraftChange(event.target.value)}
            onKeyDown={handleKeyDown}
            disabled={!canUseText}
            rows={1}
          />
          <div className="composer-actions">
            <LiveVoiceControls
              liveStatus={liveStatus}
              busyChat={busyChat}
              sessionId={sessionId}
              onToggleLive={onToggleLive}
              onPrewarmLive={onPrewarmLive}
            />
            <SendButton busyChat={busyChat} canSend={canSend} onSend={onSendMessage} onCancel={onCancelChat} />
          </div>
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
