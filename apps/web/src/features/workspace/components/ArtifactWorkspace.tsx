import { ReactNode, useEffect, useState } from "react";
import { Brain, Download, ExternalLink, FileText, FileUp, Image, Trash2, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { Asset } from "../../../types";
import { DataPreview, isPreviewableTextAsset } from "./messages/DataPreview";

type BlobPreviewState = {
  status: "idle" | "loading" | "loaded" | "error";
  url: string;
  text?: string;
  error?: string;
};

type ArtifactWorkspaceProps = {
  className?: string;
  artifact: Asset | null;
  memoryBusy: Record<string, boolean>;
  memoryDisabled: boolean;
  onClose: () => void;
  onOpenPreview: (asset: Asset) => void;
  onDownload: (id: string) => void;
  onDelete: (id: string) => void;
  onExtractMemory: (asset: Asset) => void;
  loadArtifact: (asset: Asset) => Promise<Blob>;
  loadPreview: (asset: Asset) => Promise<Blob>;
  formatBytes: (bytes: number) => string;
  formatTime: (value?: string) => string;
};

export function ArtifactWorkspace({
  className = "",
  artifact,
  memoryBusy,
  memoryDisabled,
  onClose,
  onOpenPreview,
  onDownload,
  onDelete,
  onExtractMemory,
  loadArtifact,
  loadPreview,
  formatBytes,
  formatTime
}: ArtifactWorkspaceProps) {
  return (
    <aside className={`artifact-workspace ${className}`.trim()} aria-label="Artifact preview">
      <header className="artifact-workspace-head">
        <div>
          <strong>Artifact Preview</strong>
          <small>{artifact ? artifact.filename : "No artifact selected"}</small>
        </div>
        <Button className="icon ghost" onClick={onClose} title="Close artifact preview" aria-label="Close artifact preview">
          <X size={18} />
        </Button>
      </header>
      <div className="artifact-workspace-body">
        <div className="artifact-workspace-preview">
          {artifact ? (
            <>
              <div className="artifact-workspace-preview-head">
                <div>
                  <strong>{artifact.filename}</strong>
                  <small>{artifact.content_type || "file"} · {formatBytes(artifact.size_bytes)}</small>
                </div>
                <div className="artifact-workspace-actions">
                  <Button className="icon" onClick={() => onOpenPreview(artifact)} title={`Open preview for ${artifact.filename}`} aria-label={`Open preview for ${artifact.filename}`}>
                    <ExternalLink size={16} />
                  </Button>
                  <Button className="icon" onClick={() => onDownload(artifact.id)} title={`Download ${artifact.filename}`} aria-label={`Download ${artifact.filename}`}>
                    <Download size={16} />
                  </Button>
                  <Button
                    className="icon"
                    disabled={memoryDisabled || Boolean(memoryBusy[artifact.id])}
                    onClick={() => onExtractMemory(artifact)}
                    title={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${artifact.filename}`}
                    aria-label={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${artifact.filename}`}
                  >
                    <Brain size={16} />
                  </Button>
                  <Button className="icon danger" onClick={() => onDelete(artifact.id)} title={`Delete ${artifact.filename}`} aria-label={`Delete ${artifact.filename}`}>
                    <Trash2 size={16} />
                  </Button>
                </div>
              </div>
              <ArtifactMetadata asset={artifact} formatBytes={formatBytes} formatTime={formatTime} />
              <ArtifactPreviewSurface
                asset={artifact}
                loadArtifact={() => loadArtifact(artifact)}
                loadPreview={() => loadPreview(artifact)}
              />
            </>
          ) : (
            <div className="artifact-workspace-empty">
              <Image size={32} />
              <strong>No artifact selected</strong>
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function ArtifactMetadata({
  asset,
  formatBytes,
  formatTime
}: {
  asset: Asset;
  formatBytes: (bytes: number) => string;
  formatTime: (value?: string) => string;
}) {
  const metadata = [
    ["Created", formatTime(asset.created_at)],
    ["Size", formatBytes(asset.size_bytes)],
    ["Type", asset.content_type || "file"],
    ["Job", asset.job_id || ""],
    ["Artifact ID", asset.id]
  ].filter(([, value]) => value);
  return (
    <dl className="artifact-workspace-metadata" aria-label="Artifact metadata">
      {metadata.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function ArtifactPreviewSurface({
  asset,
  loadArtifact,
  loadPreview
}: {
  asset: Asset;
  loadArtifact: () => Promise<Blob>;
  loadPreview: () => Promise<Blob>;
}) {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  const isImage = isImageAsset(asset);
  const isPDF = isPDFAsset(asset);
  const isText = isPreviewableTextAsset(asset);
  const isDocx = isDOCXAsset(asset);
  const isOffice = ["ppt", "pptx", "doc", "docx", "xls", "xlsx"].includes(ext);
  const [assetPreview, setAssetPreview] = useState<BlobPreviewState>({ status: "idle", url: "" });
  const [docxPreview, setDocxPreview] = useState<BlobPreviewState>({ status: "idle", url: "" });

  useEffect(() => {
    let cancelled = false;
    let objectUrl = "";
    setAssetPreview({ status: "loading", url: "" });
    loadArtifact()
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
  }, [asset.id, isText]);

  useEffect(() => {
    if (!isDocx) {
      setDocxPreview({ status: "idle", url: "" });
      return;
    }
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
  }, [asset.id, isDocx]);

  return (
    <div className="artifact-preview-surface">
      {assetPreview.status === "loading" && !isText && !isDocx && <PreviewFallback>Loading preview...</PreviewFallback>}
      {assetPreview.status === "error" && !isText && !isDocx && <PreviewFallback>{assetPreview.error || "Preview failed"}</PreviewFallback>}
      {isImage && assetPreview.status === "loaded" && <img src={assetPreview.url} alt={asset.filename} />}
      {isPDF && assetPreview.status === "loaded" && <iframe src={assetPreview.url} title={asset.filename} />}
      {isDocx && docxPreview.status === "loading" && <PreviewFallback>Loading preview...</PreviewFallback>}
      {isDocx && docxPreview.status === "error" && <PreviewFallback>{docxPreview.error || "Preview failed"}</PreviewFallback>}
      {isDocx && docxPreview.status === "loaded" && <iframe src={docxPreview.url} title={`${asset.filename} preview`} />}
      {isText && (
        <div className="artifact-text-preview" role="document" aria-label={asset.filename}>
          {assetPreview.status === "loading" && <PreviewFallback>Loading preview...</PreviewFallback>}
          {assetPreview.status === "error" && <PreviewFallback>{assetPreview.error || "Preview failed"}</PreviewFallback>}
          {assetPreview.status === "loaded" && <DataPreview text={assetPreview.text || ""} filename={asset.filename} contentType={asset.content_type} />}
        </div>
      )}
      {isOffice && !isDocx && (
        <PreviewFallback>
          <FileUp size={30} />
          <strong>{asset.filename}</strong>
          <span>Use download or expanded preview for this file.</span>
        </PreviewFallback>
      )}
      {!isImage && !isPDF && !isText && !isDocx && !isOffice && (
        <PreviewFallback>
          <FileUp size={30} />
          <strong>{asset.filename}</strong>
        </PreviewFallback>
      )}
    </div>
  );
}

function PreviewFallback({ children }: { children: ReactNode }) {
  return <div className="artifact-preview-fallback">{children}</div>;
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

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
