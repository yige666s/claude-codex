import { Ref, RefObject } from "react";
import { FileUp } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/input";

type ComposerUploadButtonProps = {
  inputRef: RefObject<HTMLInputElement | null>;
  uploading: boolean;
  disabled: boolean;
  accept: string;
  onUpload: (files: FileList | null) => void;
};

export function ComposerUploadButton({ inputRef, uploading, disabled, accept, onUpload }: ComposerUploadButtonProps) {
  return (
    <>
      <Button
        type="button"
        className="composer-upload"
        variant="outline"
        size="icon-lg"
        title="Upload attachment"
        aria-label="Upload attachment"
        onClick={() => inputRef.current?.click()}
        disabled={uploading || disabled}
      >
        <FileUp size={18} />
      </Button>
      <Input
        ref={inputRef as Ref<HTMLInputElement>}
        className="composer-file-input"
        type="file"
        tabIndex={-1}
        aria-hidden="true"
        accept={accept}
        onChange={(event) => onUpload(event.currentTarget.files)}
        disabled={uploading || disabled}
      />
    </>
  );
}
