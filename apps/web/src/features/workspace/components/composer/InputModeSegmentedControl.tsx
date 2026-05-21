import { MessageCircle, Mic } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import type { InputMode } from "../../hooks/useLiveVoice";

type InputModeSegmentedControlProps = {
  inputMode: InputMode;
  busyChat: boolean;
  sessionId: string;
  onSwitchToText: () => void;
  onSwitchToLive: () => void;
};

export function InputModeSegmentedControl({
  inputMode,
  busyChat,
  sessionId,
  onSwitchToText,
  onSwitchToLive
}: InputModeSegmentedControlProps) {
  return (
    <div className="input-mode-toggle" role="group" aria-label="Input mode">
      <Button
        type="button"
        className={inputMode === "text" ? "active" : ""}
        variant={inputMode === "text" ? "secondary" : "ghost"}
        size="sm"
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
        variant={inputMode === "live" ? "secondary" : "ghost"}
        size="sm"
        onClick={onSwitchToLive}
        disabled={!sessionId || busyChat}
        title="Live mode"
        aria-label="Live mode"
      >
        <Mic size={16} />
        <span>Live</span>
      </Button>
    </div>
  );
}
