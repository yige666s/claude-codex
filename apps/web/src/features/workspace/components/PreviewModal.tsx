import { useEffect, useState } from "react";
import { Download, FileUp, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../../components/ui/dialog";
import { userFacingErrorMessage } from "../../../api/errorMessages";
import type { Asset } from "../../../types";
import { DataPreview, isPreviewableTextAsset } from "./messages/DataPreview";

type BlobPreviewState = {
  status: "idle" | "loading" | "loaded" | "error";
  url: string;
  text?: string;
  error?: string;
};

type PreviewModalProps = {
  asset: Asset;
  loadAsset: () => Promise<Blob>;
  loadPreview?: () => Promise<Blob>;
  onClose: () => void;
};

export function PreviewModal({ asset, loadAsset, loadPreview, onClose }: PreviewModalProps) {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  const isImage = isImageAsset(asset);
  const isPDF = isPDFAsset(asset);
  const isText = isPreviewableTextAsset(asset);
  const isDocx = isDOCXAsset(asset);
  const isOffice = ["ppt", "pptx", "doc", "docx", "xls", "xlsx"].includes(ext);
  const [assetPreview, setAssetPreview] = useState<BlobPreviewState>({
    status: "idle",
    url: ""
  });
  const [docxPreview, setDocxPreview] = useState<BlobPreviewState>({
    status: "idle",
    url: ""
  });

  useEffect(() => {
    let cancelled = false;
    let objectUrl = "";
    setAssetPreview({ status: "loading", url: "" });
    loadAsset()
      .then(async (blob) => {
        objectUrl = URL.createObjectURL(blob);
        const text = isText ? await blob.text() : undefined;
        if (!cancelled) setAssetPreview({ status: "loaded", url: objectUrl, text });
      })
      .catch((error) => {
        if (!cancelled) setAssetPreview({ status: "error", url: "", error: errorMessage(error) });
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [isText, loadAsset]);

  useEffect(() => {
    if (!isDocx || !loadPreview) return;
    let cancelled = false;
    let objectUrl = "";
    setDocxPreview({ status: "loading", url: "" });
    loadPreview()
      .then((blob) => {
        objectUrl = URL.createObjectURL(blob);
        if (!cancelled) setDocxPreview({ status: "loaded", url: objectUrl });
      })
      .catch((error) => {
        if (!cancelled) setDocxPreview({ status: "error", url: "", error: errorMessage(error) });
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [isDocx, loadPreview]);

  const downloadReady = assetPreview.status === "loaded" && assetPreview.url;

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
            <Button
              className="preview-download"
              disabled={!downloadReady}
              onClick={() => downloadObjectURL(assetPreview.url, asset.filename)}
              title={`Download ${asset.filename}`}
              aria-label={`Download ${asset.filename}`}
            >
              <Download size={16} />
              <span>Download</span>
            </Button>
            <Button className="icon ghost" onClick={onClose} title="Close preview" aria-label="Close preview"><X size={18} /></Button>
          </div>
        </header>
        <div className="preview-body">
          {assetPreview.status === "loading" && !isText && !isDocx && <div className="preview-fallback">Loading preview...</div>}
          {assetPreview.status === "error" && !isText && !isDocx && <div className="preview-fallback">{assetPreview.error || "Preview failed"}</div>}
          {isImage && assetPreview.status === "loaded" && <img src={assetPreview.url} alt={asset.filename} />}
          {isPDF && assetPreview.status === "loaded" && <iframe src={assetPreview.url} title={asset.filename} />}
          {isDocx && docxPreview.status === "loading" && <div className="preview-fallback">Loading preview...</div>}
          {isDocx && docxPreview.status === "error" && <div className="preview-fallback">{docxPreview.error || "Preview failed"}</div>}
          {isDocx && docxPreview.status === "loaded" && <iframe src={docxPreview.url} title={`${asset.filename} preview`} />}
          {isText && (
            <div className="text-preview" role="document" aria-label={asset.filename}>
              {assetPreview.status === "loading" && <div className="preview-fallback">Loading preview...</div>}
              {assetPreview.status === "error" && <div className="preview-fallback">{assetPreview.error || "Preview failed"}</div>}
              {assetPreview.status === "loaded" && <DataPreview text={assetPreview.text || ""} filename={asset.filename} contentType={asset.content_type} />}
            </div>
          )}
          {isOffice && (!isDocx || !loadPreview) && (
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

function formatBytes(bytes: number): string {
  if (!bytes) return "0 KB";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function downloadObjectURL(url: string, filename: string): void {
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? userFacingErrorMessage(error.message) : userFacingErrorMessage(String(error));
}
