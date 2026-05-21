import { useEffect, useState } from "react";
import { Download, FileUp, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../../components/ui/dialog";
import type { Asset } from "../../../types";

export function PreviewModal({ asset, url, previewUrl, onClose }: { asset: Asset; url: string; previewUrl?: string; onClose: () => void }) {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  const isImage = isImageAsset(asset);
  const isPDF = isPDFAsset(asset);
  const isText = isTextAsset(asset);
  const isDocx = isDOCXAsset(asset);
  const isOffice = ["ppt", "pptx", "doc", "docx", "xls", "xlsx"].includes(ext);
  const [textPreview, setTextPreview] = useState<{ status: "idle" | "loading" | "loaded" | "error"; content: string; error?: string }>({
    status: "idle",
    content: ""
  });

  useEffect(() => {
    if (!isText) return;
    let cancelled = false;
    setTextPreview({ status: "loading", content: "" });
    fetch(url, { credentials: "include", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error(`Preview failed (${response.status})`);
        return response.text();
      })
      .then((content) => {
        if (!cancelled) setTextPreview({ status: "loaded", content });
      })
      .catch((error) => {
        if (!cancelled) setTextPreview({ status: "error", content: "", error: errorMessage(error) });
      });
    return () => {
      cancelled = true;
    };
  }, [isText, url]);

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onClose();
    }}>
      <DialogContent className="preview-modal" hideClose>
        <DialogTitle className="sr-only">{asset.filename}</DialogTitle>
        <DialogDescription className="sr-only">Preview and download this file.</DialogDescription>
        <header>
          <div>
            <strong id="preview-title">{asset.filename}</strong>
            <small>{asset.content_type || "file"} · {formatBytes(asset.size_bytes)}</small>
          </div>
          <div className="preview-actions">
            <Button className="preview-download" onClick={() => window.open(url, "_blank")} title={`Download ${asset.filename}`} aria-label={`Download ${asset.filename}`}>
              <Download size={16} />
              <span>Download</span>
            </Button>
            <Button className="icon ghost" onClick={onClose} title="Close preview" aria-label="Close preview"><X size={18} /></Button>
          </div>
        </header>
        <div className="preview-body">
          {isImage && <img src={url} alt={asset.filename} />}
          {isPDF && <iframe src={url} title={asset.filename} />}
          {isDocx && previewUrl && <iframe src={previewUrl} title={`${asset.filename} preview`} />}
          {isText && (
            <div className="text-preview" role="document" aria-label={asset.filename}>
              {textPreview.status === "loading" && <div className="preview-fallback">Loading preview...</div>}
              {textPreview.status === "error" && <div className="preview-fallback">{textPreview.error || "Preview failed"}</div>}
              {textPreview.status === "loaded" && <pre>{textPreview.content}</pre>}
            </div>
          )}
          {isOffice && (!isDocx || !previewUrl) && (
            <div className="preview-fallback">
              <FileUp size={32} />
              <strong>{asset.filename}</strong>
              <p>Office previews depend on the browser or deployment viewer. Use download/open for this file.</p>
            </div>
          )}
          {!isImage && !isPDF && !isText && !isDocx && !isOffice && (
            <div className="preview-fallback">
              <FileUp size={32} />
              <strong>{asset.filename}</strong>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function isImageAsset(asset: Asset): boolean {
  return (asset.content_type || "").startsWith("image/");
}

function isPDFAsset(asset: Asset): boolean {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return asset.content_type === "application/pdf" || ext === "pdf";
}

function isDOCXAsset(asset: Asset): boolean {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return asset.content_type === "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || ext === "docx";
}

function isTextAsset(asset: Asset): boolean {
  const contentType = asset.content_type || "";
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return (
    contentType.startsWith("text/") ||
    [
      "txt",
      "md",
      "markdown",
      "csv",
      "tsv",
      "json",
      "jsonl",
      "log",
      "yaml",
      "yml",
      "xml",
      "html",
      "css",
      "js",
      "jsx",
      "ts",
      "tsx",
      "go",
      "py",
      "java",
      "c",
      "cpp",
      "h",
      "sh",
      "sql",
      "toml",
      "ini",
      "env"
    ].includes(ext)
  );
}


function formatBytes(bytes: number): string {
  if (!bytes) return "0 KB";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
