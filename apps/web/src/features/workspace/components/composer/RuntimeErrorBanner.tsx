import { AlertCircle, X } from "lucide-react";
import { Alert } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";

type RuntimeErrorBannerProps = {
  message: string;
  upload?: boolean;
  onDismiss: () => void;
};

export function RuntimeErrorBanner({ message, upload, onDismiss }: RuntimeErrorBannerProps) {
  if (!message) return null;
  return (
    <Alert className={`composer-error ${upload ? "upload-error" : ""}`} variant="destructive">
      {!upload && <AlertCircle size={16} />}
      <span>{message}</span>
      <Button className="icon" variant="ghost" size="icon-sm" onClick={onDismiss} title="Dismiss error" aria-label="Dismiss error">
        <X size={14} />
      </Button>
    </Alert>
  );
}
