import { Send, Square } from "lucide-react";
import { Button } from "../../../../components/ui/button";

type SendButtonProps = {
  busyChat: boolean;
  canSend: boolean;
  onSend: () => void;
  onCancel: () => void;
};

export function SendButton({ busyChat, canSend, onSend, onCancel }: SendButtonProps) {
  if (busyChat) {
    return (
      <Button className="stop-generation" variant="destructive" size="icon-lg" onClick={onCancel} title="Stop generation" aria-label="Stop generation">
        <span><Square size={16} fill="currentColor" /></span>
      </Button>
    );
  }
  return (
    <Button className="send" variant="primary" size="icon-lg" onClick={onSend} disabled={!canSend} title="Send" aria-label="Send">
      <Send size={21} />
    </Button>
  );
}
