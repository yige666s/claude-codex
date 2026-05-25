import { FocusEvent, useRef, useState } from "react";
import { Mic, MicOff, Volume2, VolumeX } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "../../../../components/ui/popover";
import { Slider } from "../../../../components/ui/slider";
import { Tooltip, TooltipContent, TooltipTrigger } from "../../../../components/ui/tooltip";
import type { InputMode, LiveStatus } from "../../hooks/useLiveVoice";

type LiveVoiceControlsProps = {
  inputMode: InputMode;
  liveStatus: LiveStatus;
  liveMuted: boolean;
  speakerVolume: number;
  micVolume: number;
  busyChat: boolean;
  sessionId: string;
  onToggleSpeakerMute: () => void;
  onToggleMicMute: () => void;
  onSpeakerVolumeChange: (value: number) => void;
  onMicVolumeChange: (value: number) => void;
};

export function LiveVoiceControls({
  inputMode,
  liveStatus,
  liveMuted,
  speakerVolume,
  micVolume,
  busyChat,
  sessionId,
  onToggleSpeakerMute,
  onToggleMicMute,
  onSpeakerVolumeChange,
  onMicVolumeChange
}: LiveVoiceControlsProps) {
  if (inputMode !== "live") return null;
  const micCapturing = liveStatus === "listening" || liveStatus === "speaking" || liveStatus === "thinking";
  const micDisabled = !sessionId || busyChat || liveStatus === "connecting" || liveStatus === "reconnecting" || liveStatus === "error";
  const micTooltip = micCapturing ? "Mute microphone" : liveStatus === "reconnecting" ? "Reconnecting microphone" : "Unmute microphone";
  return (
    <>
      <VolumePopoverControl
        className="speaker"
        label="Voice output volume"
        tooltip={liveMuted ? "Unmute speaker" : "Mute speaker"}
        buttonClassName={`voice-output-toggle ${liveMuted ? "muted" : ""}`}
        buttonVariant={liveMuted ? "destructive" : "outline"}
        disabled={liveStatus === "idle" || liveStatus === "connecting" || !sessionId}
        pressed={liveStatus !== "idle" && !liveMuted}
        value={liveMuted ? 0 : speakerVolume}
        onValueChange={onSpeakerVolumeChange}
        onToggle={onToggleSpeakerMute}
        icon={liveMuted ? <VolumeX size={18} /> : <Volume2 size={18} />}
      />
      <VolumePopoverControl
        className="mic"
        label="Microphone volume"
        tooltip={micTooltip}
        buttonClassName={`live-control ${micCapturing ? "active" : ""}`}
        buttonVariant={micCapturing ? "destructive" : "outline"}
        disabled={micDisabled}
        pressed={micCapturing}
        value={micCapturing ? micVolume : 0}
        onValueChange={onMicVolumeChange}
        onToggle={onToggleMicMute}
        icon={micCapturing ? <Mic size={18} /> : <MicOff size={18} />}
      />
    </>
  );
}

type VolumePopoverControlProps = {
  className: string;
  label: string;
  tooltip: string;
  buttonClassName: string;
  buttonVariant: "outline" | "destructive";
  disabled: boolean;
  pressed: boolean;
  value: number;
  icon: JSX.Element;
  onValueChange: (value: number) => void;
  onToggle: () => void;
};

function VolumePopoverControl({
  className,
  label,
  tooltip,
  buttonClassName,
  buttonVariant,
  disabled,
  pressed,
  value,
  icon,
  onValueChange,
  onToggle
}: VolumePopoverControlProps) {
  const [open, setOpen] = useState(false);
  const closeTimerRef = useRef<number | null>(null);
  const clearCloseTimer = () => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  };
  const openControl = () => {
    clearCloseTimer();
    setOpen(true);
  };
  const scheduleClose = () => {
    clearCloseTimer();
    closeTimerRef.current = window.setTimeout(() => setOpen(false), 120);
  };
  const closeOnBlur = (event: FocusEvent<HTMLDivElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
      scheduleClose();
    }
  };

  return (
    <div
      className={`live-volume-control ${className}`}
      onMouseEnter={openControl}
      onMouseLeave={scheduleClose}
      onFocusCapture={openControl}
      onBlurCapture={closeOnBlur}
    >
      <Popover open={open} onOpenChange={setOpen}>
        <Tooltip>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                type="button"
                className={buttonClassName}
                variant={buttonVariant}
                size="icon-lg"
                onClick={onToggle}
                disabled={disabled}
                aria-label={tooltip}
                aria-pressed={pressed}
              >
                {icon}
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
        <PopoverContent
          className="live-volume-popover"
          align="center"
          side="top"
          sideOffset={10}
          onMouseEnter={openControl}
          onMouseLeave={scheduleClose}
          onFocusCapture={openControl}
          onBlurCapture={closeOnBlur}
        >
          <Slider
            orientation="vertical"
            min={0}
            max={100}
            step={1}
            value={[Math.round(value * 100)]}
            onValueChange={(next) => onValueChange((next[0] || 0) / 100)}
            aria-label={label}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}
