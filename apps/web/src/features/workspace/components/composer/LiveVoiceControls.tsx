import { Mic, MicOff } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "../../../../components/ui/tooltip";
import type { LiveStatus } from "../../hooks/useLiveVoice";

type LiveVoiceControlsProps = {
  liveStatus: LiveStatus;
  busyChat: boolean;
  sessionId: string;
  onToggleLive: () => void;
  onPrewarmLive: () => void;
};

export function LiveVoiceControls({
  liveStatus,
  busyChat,
  sessionId,
  onToggleLive,
  onPrewarmLive
}: LiveVoiceControlsProps) {
  const liveActive = liveStatus !== "idle" && liveStatus !== "error";
  const disabled = !sessionId || (!liveActive && busyChat);
  const tooltip = liveActive ? "Stop Live voice" : "Start Live voice";

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          className={`live-control ${liveActive ? "active" : ""}`}
          variant={liveActive ? "destructive" : "outline"}
          size="icon-lg"
          onClick={onToggleLive}
          onPointerDown={onPrewarmLive}
          disabled={disabled}
          aria-label={tooltip}
          aria-pressed={liveActive}
        >
          {liveActive ? <Mic size={18} /> : <MicOff size={18} />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
