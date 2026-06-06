import { ReactNode, useEffect, useMemo, useState } from "react";
import { Brain, Download, ExternalLink, FileText, FileUp, Image, Search, Trash2, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import type { Asset } from "../../../types";
import { MarkdownContent } from "./messages/MarkdownContent";

type BlobPreviewState = {
  status: "idle" | "loading" | "loaded" | "error";
  url: string;
  text?: string;
  error?: string;
};

type ArtifactWorkspaceProps = {
  className?: string;
  artifacts: Asset[];
  selectedArtifactId: string;
  memoryBusy: Record<string, boolean>;
  memoryDisabled: boolean;
  onSelectArtifact: (id: string) => void;
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
  artifacts,
  selectedArtifactId,
  memoryBusy,
  memoryDisabled,
  onSelectArtifact,
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
  const [query, setQuery] = useState("");
  const selectedArtifact = artifacts.find((asset) => asset.id === selectedArtifactId) || artifacts[0] || null;
  const filteredArtifacts = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return artifacts;
    return artifacts.filter((asset) => [asset.filename, asset.content_type, asset.job_id, asset.id].some((value) => value?.toLowerCase().includes(needle)));
  }, [artifacts, query]);

  return (
    <aside className={`artifact-workspace ${className}`.trim()} aria-label="Artifact workspace">
      <header className="artifact-workspace-head">
        <div>
          <strong>Artifacts</strong>
          <small>{artifacts.length ? `${artifacts.length} generated item${artifacts.length === 1 ? "" : "s"}` : "No generated items"}</small>
        </div>
        <Button className="icon ghost" onClick={onClose} title="Close artifact workspace" aria-label="Close artifact workspace">
          <X size={18} />
        </Button>
      </header>
      <div className="artifact-workspace-search">
        <Search size={16} />
        <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search artifacts" aria-label="Search artifacts" />
      </div>
      <div className="artifact-workspace-body">
        <div className="artifact-workspace-list" role="list" aria-label="Artifacts">
          {!filteredArtifacts.length && <div className="empty-small">{query ? "No matching artifacts" : "No artifacts yet"}</div>}
          {filteredArtifacts.map((asset) => (
            <button
              key={asset.id}
              className={`artifact-workspace-item ${selectedArtifact?.id === asset.id ? "active" : ""}`}
              onClick={() => onSelectArtifact(asset.id)}
              type="button"
            >
              <ArtifactIcon asset={asset} />
              <span>
                <strong>{asset.filename}</strong>
                <small>{formatBytes(asset.size_bytes)} · {formatTime(asset.created_at)}</small>
              </span>
            </button>
          ))}
        </div>
        <div className="artifact-workspace-preview">
          {selectedArtifact ? (
            <>
              <div className="artifact-workspace-preview-head">
                <div>
                  <strong>{selectedArtifact.filename}</strong>
                  <small>{selectedArtifact.content_type || "file"} · {formatBytes(selectedArtifact.size_bytes)}</small>
                </div>
                <div className="artifact-workspace-actions">
                  <Button className="icon" onClick={() => onOpenPreview(selectedArtifact)} title={`Open preview for ${selectedArtifact.filename}`} aria-label={`Open preview for ${selectedArtifact.filename}`}>
                    <ExternalLink size={16} />
                  </Button>
                  <Button className="icon" onClick={() => onDownload(selectedArtifact.id)} title={`Download ${selectedArtifact.filename}`} aria-label={`Download ${selectedArtifact.filename}`}>
                    <Download size={16} />
                  </Button>
                  <Button
                    className="icon"
                    disabled={memoryDisabled || Boolean(memoryBusy[selectedArtifact.id])}
                    onClick={() => onExtractMemory(selectedArtifact)}
                    title={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${selectedArtifact.filename}`}
                    aria-label={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${selectedArtifact.filename}`}
                  >
                    <Brain size={16} />
                  </Button>
                  <Button className="icon danger" onClick={() => onDelete(selectedArtifact.id)} title={`Delete ${selectedArtifact.filename}`} aria-label={`Delete ${selectedArtifact.filename}`}>
                    <Trash2 size={16} />
                  </Button>
                </div>
              </div>
              <ArtifactPreviewSurface
                asset={selectedArtifact}
                loadArtifact={() => loadArtifact(selectedArtifact)}
                loadPreview={() => loadPreview(selectedArtifact)}
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
  const isText = isTextAsset(asset);
  const isMarkdown = isMarkdownAsset(asset);
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
          {assetPreview.status === "loaded" && (
            isMarkdown ? <MarkdownContent text={assetPreview.text || ""} /> : <pre>{assetPreview.text}</pre>
          )}
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

function ArtifactIcon({ asset }: { asset: Asset }) {
  if (isImageAsset(asset)) return <Image size={17} />;
  if (isTextAsset(asset) || isPDFAsset(asset)) return <FileText size={17} />;
  return <FileUp size={17} />;
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
    ["txt", "md", "markdown", "csv", "tsv", "json", "jsonl", "log", "yaml", "yml", "xml", "html", "css", "js", "jsx", "ts", "tsx", "go", "py", "java", "c", "cpp", "h", "sh", "sql", "toml", "ini", "env"].includes(ext)
  );
}

function isMarkdownAsset(asset: Asset): boolean {
  const contentType = (asset.content_type || "").toLowerCase().split(";")[0].trim();
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return contentType === "text/markdown" || ext === "md" || ext === "markdown";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
