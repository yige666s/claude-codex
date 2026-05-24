import { Bot, Check, ChevronDown, Mic } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger
} from "../../../../components/ui/dropdown-menu";
import type { InputMode } from "../../hooks/useLiveVoice";

type ToolModeSelectorProps = {
  inputMode: InputMode;
  busyChat: boolean;
  sessionId: string;
  onSelectChat: () => void;
  onSelectLive: () => void;
};

export function ToolModeSelector({
  inputMode,
  busyChat,
  sessionId,
  onSelectChat,
  onSelectLive
}: ToolModeSelectorProps) {
  const activeKind = inputMode === "live" ? "live" : "chat";
  const label = activeKind === "live" ? "Live" : "Chat";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          className={`tool-mode-trigger ${activeKind}`}
          disabled={busyChat}
          title="Choose mode"
          aria-label="Choose mode"
        >
          {activeKind === "live" ? <Mic size={16} /> : <Bot size={16} />}
          <span>{label}</span>
          <ChevronDown size={14} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="tool-mode-menu" align="end" side="top" sideOffset={8}>
        <DropdownMenuLabel>Mode</DropdownMenuLabel>
        <DropdownMenuItem onClick={onSelectChat}>
          <Bot size={16} />
          <span>Chat</span>
          {activeKind === "chat" && <Check className="tool-mode-check" size={15} />}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onSelectLive} disabled={!sessionId || busyChat}>
          <Mic size={16} />
          <span>Live</span>
          {activeKind === "live" && <Check className="tool-mode-check" size={15} />}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
