import { ReactNode, useEffect, useState } from "react";
import { Brain, Download, ExternalLink, FileUp, Image, Trash2, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { Asset, JobEvent } from "../../../types";
import { DataPreview, isPreviewableTextAsset } from "./messages/DataPreview";
import { buildParallelGroupsFromJobEvents, type ParallelGroupTrace } from "./parallelTrace";

type BlobPreviewState = {
  status: "idle" | "loading" | "loaded" | "error";
  url: string;
  text?: string;
  error?: string;
};

type ArtifactWorkspaceProps = {
  className?: string;
  artifact: Asset | null;
  jobEvents?: JobEvent[];
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
  jobEvents = [],
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
              <ArtifactParallelContribution groups={buildParallelGroupsFromJobEvents(jobEvents)} />
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

function ArtifactParallelContribution({ groups }: { groups: ParallelGroupTrace[] }) {
  const branchCount = groups.reduce((sum, group) => sum + group.branches.length, 0);
  if (branchCount === 0) return null;
  return (
    <section className="artifact-parallel-contribution" aria-label="Parallel branch contribution">
      <header>
        <div>
          <strong>Parallel contribution</strong>
          <small>{branchCount} branches · {groups.reduce((sum, group) => sum + group.sources.length, 0)} sources</small>
        </div>
      </header>
      <div className="artifact-parallel-quality">
        {groups.map((group) => (
          <div key={group.id} className="parallel-quality-strip">
            {typeof group.coverageScore === "number" && <span className="parallel-quality-pill">{Math.round(group.coverageScore * 100)}% coverage</span>}
            <span className={`parallel-quality-pill ${group.missingCoverage.length ? "warning" : ""}`}>
              {group.missingCoverage.length ? `${group.missingCoverage.length} missing` : "complete"}
            </span>
            <span className={`parallel-quality-pill ${group.conflictCount ? "warning" : ""}`}>
              {group.conflictCount ? `${group.conflictCount} conflicts` : "no conflicts"}
            </span>
            {group.missingCoverage.length > 0 && <small>Missing: {group.missingCoverage.join(", ")}</small>}
            {group.conflicts.slice(0, 2).map((conflict) => (
              <small key={`${group.id}-${conflict.field}-${conflict.subject || "default"}`}>
                Conflict: {conflict.field}{conflict.subject ? `/${conflict.subject}` : ""} · {conflict.values.join(" vs ")}
              </small>
            ))}
          </div>
        ))}
      </div>
      <div className="artifact-parallel-branches">
        {groups.flatMap((group) => group.branches).map((branch) => (
          <details key={branch.id} className={`artifact-parallel-branch ${branch.status}`}>
            <summary>
              <span>{branch.title}</span>
              <small>{branch.status} · {branch.sourceCount} sources</small>
            </summary>
            {branch.sources.length > 0 ? (
              <div className="parallel-source-chips">
                {branch.sources.slice(0, 8).map((source, index) => source.url ? (
                  <a key={source.url} href={source.url} target="_blank" rel="noreferrer">{source.title || source.provider || `Source ${index + 1}`}</a>
                ) : (
                  <span key={`${source.title || source.provider}-${index}`}>{source.title || source.provider || `Source ${index + 1}`}</span>
                ))}
              </div>
            ) : (
              <p>No branch sources were recorded.</p>
            )}
            {branch.error && <p className="artifact-parallel-error">{branch.error}</p>}
          </details>
        ))}
      </div>
    </section>
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
  const isOfficePreview = isOfficePreviewAsset(asset);
  const isOffice = ["ppt", "pptx", "doc", "docx", "xls", "xlsx"].includes(ext);
  const [assetPreview, setAssetPreview] = useState<BlobPreviewState>({ status: "idle", url: "" });
  const [docxPreview, setDocxPreview] = useState<BlobPreviewState>({ status: "idle", url: "" });
  const [docxFrameHeight, setDocxFrameHeight] = useState(360);

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
    if (!isOfficePreview) {
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
  }, [asset.id, isOfficePreview]);

  useEffect(() => {
    setDocxFrameHeight(360);
  }, [asset.id]);

  function resizeDocxFrame(frame: HTMLIFrameElement) {
    const nextHeight = docxFrameContentHeight(frame);
    if (nextHeight > 0) setDocxFrameHeight(nextHeight);
  }

  return (
    <div className={`artifact-preview-surface ${isOfficePreview ? "office" : ""}`.trim()}>
      {assetPreview.status === "loading" && !isText && !isOfficePreview && <PreviewFallback>Loading preview...</PreviewFallback>}
      {assetPreview.status === "error" && !isText && !isOfficePreview && <PreviewFallback>{assetPreview.error || "Preview failed"}</PreviewFallback>}
      {isImage && assetPreview.status === "loaded" && <img src={assetPreview.url} alt={asset.filename} />}
      {isPDF && assetPreview.status === "loaded" && <iframe src={assetPreview.url} title={asset.filename} />}
      {isOfficePreview && docxPreview.status === "loading" && <PreviewFallback>Loading preview...</PreviewFallback>}
      {isOfficePreview && docxPreview.status === "error" && <PreviewFallback>{docxPreview.error || "Preview failed"}</PreviewFallback>}
      {isOfficePreview && docxPreview.status === "loaded" && (
        <iframe
          className="docx-preview-frame"
          src={docxPreview.url}
          style={{ height: `${docxFrameHeight}px` }}
          title={`${asset.filename} preview`}
          onLoad={(event) => resizeDocxFrame(event.currentTarget)}
        />
      )}
      {isText && (
        <div className="artifact-text-preview" role="document" aria-label={asset.filename}>
          {assetPreview.status === "loading" && <PreviewFallback>Loading preview...</PreviewFallback>}
          {assetPreview.status === "error" && <PreviewFallback>{assetPreview.error || "Preview failed"}</PreviewFallback>}
          {assetPreview.status === "loaded" && <DataPreview text={assetPreview.text || ""} filename={asset.filename} contentType={asset.content_type} />}
        </div>
      )}
      {isOffice && !isOfficePreview && (
        <PreviewFallback>
          <FileUp size={30} />
          <strong>{asset.filename}</strong>
          <span>Use download or expanded preview for this file.</span>
        </PreviewFallback>
      )}
      {!isImage && !isPDF && !isText && !isOfficePreview && !isOffice && (
        <PreviewFallback>
          <FileUp size={30} />
          <strong>{asset.filename}</strong>
        </PreviewFallback>
      )}
    </div>
  );
}

function docxFrameContentHeight(frame: HTMLIFrameElement): number {
  const doc = frame.contentDocument;
  if (!doc) return 0;
  const body = doc.body;
  const root = doc.documentElement;
  return Math.ceil(Math.max(body?.scrollHeight || 0, body?.offsetHeight || 0, root?.scrollHeight || 0, root?.offsetHeight || 0)) + 2;
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

function isOfficePreviewAsset(asset: Asset): boolean {
  const ext = asset.filename.split(".").pop()?.toLowerCase() || "";
  return (
    asset.content_type === "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
    asset.content_type === "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
    asset.content_type === "application/vnd.openxmlformats-officedocument.presentationml.presentation" ||
    ["docx", "xlsx", "pptx"].includes(ext)
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
