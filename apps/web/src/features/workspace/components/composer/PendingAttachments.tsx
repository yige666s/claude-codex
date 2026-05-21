import { FileUp, X } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import type { Asset } from "../../../../types";

type PendingAttachmentsProps = {
  attachments: Asset[];
  onRemove: (id: string) => void;
};

export function PendingAttachments({ attachments, onRemove }: PendingAttachmentsProps) {
  if (!attachments.length) return null;
  return (
    <div className="pending-attachments" aria-label="Pending attachments">
      {attachments.map((asset) => (
        <span className="pending-attachment" key={asset.id} title={asset.filename}>
          <FileUp size={13} />
          <span>{asset.filename}</span>
          <Button
            className="icon"
            variant="ghost"
            size="icon-sm"
            onClick={() => onRemove(asset.id)}
            title={`Remove ${asset.filename}`}
            aria-label={`Remove ${asset.filename}`}
          >
            <X size={12} />
          </Button>
        </span>
      ))}
    </div>
  );
}
