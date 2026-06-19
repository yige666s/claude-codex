import { ReactNode } from "react";
import { Brain, Download, FileText, FileUp, Image, MessageSquarePlus, Trash2 } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "../../../../components/ui/tooltip";
import type { Asset } from "../../../../types";

type AssetPanelProps = {
  assets: Asset[];
  icon: "file" | "image";
  emptyLabel: string;
  uploadProgress?: number;
  openAsset?: (asset: Asset) => void;
  preview: (asset: Asset) => void;
  download: (id: string) => void;
  remove: (id: string) => void;
  extractMemory?: (asset: Asset) => void;
  memoryBusy?: Record<string, boolean>;
  memoryDisabled?: boolean;
  addToMessage?: (asset: Asset) => void;
  formatBytes: (bytes: number) => string;
  formatTime: (value?: string) => string;
};

export function AssetPanel({
  assets,
  icon,
  emptyLabel,
  uploadProgress = 0,
  openAsset,
  preview,
  download,
  remove,
  extractMemory,
  memoryBusy = {},
  memoryDisabled = false,
  addToMessage,
  formatBytes,
  formatTime
}: AssetPanelProps) {
  return (
    <>
      {uploadProgress > 0 && <Progress value={uploadProgress} />}
      <AssetList
        assets={assets}
        icon={icon}
        emptyLabel={emptyLabel}
        openAsset={openAsset}
        preview={preview}
        download={download}
        remove={remove}
        extractMemory={extractMemory}
        memoryBusy={memoryBusy}
        memoryDisabled={memoryDisabled}
        addToMessage={addToMessage}
        formatBytes={formatBytes}
        formatTime={formatTime}
      />
    </>
  );
}

function Progress({ value }: { value: number }) {
  return <div className="upload-progress"><span style={{ width: `${Math.max(0, Math.min(100, value))}%` }} /></div>;
}

function AssetList({
  assets,
  icon,
  emptyLabel,
  openAsset,
  preview,
  download,
  remove,
  extractMemory,
  memoryBusy,
  memoryDisabled,
  addToMessage,
  formatBytes,
  formatTime
}: Required<Pick<AssetPanelProps, "assets" | "icon" | "emptyLabel" | "preview" | "download" | "remove" | "memoryBusy" | "memoryDisabled" | "formatBytes" | "formatTime">> & Pick<AssetPanelProps, "openAsset" | "extractMemory" | "addToMessage">) {
  if (!assets.length) return <div className="empty-small">{emptyLabel}</div>;
  const Icon = icon === "image" ? Image : FileUp;
  return (
    <div className="asset-list">
      {assets.map((asset) => (
        <div key={asset.id} className={`asset-row ${openAsset ? "clickable" : ""} ${addToMessage ? "with-add" : ""} ${extractMemory ? "with-memory" : ""}`}>
          <AssetRowMain asset={asset} icon={<Icon size={18} />} openAsset={openAsset} formatBytes={formatBytes} formatTime={formatTime} />
          {addToMessage && (
            <IconAction label={`Add ${asset.filename} to message`} onClick={() => addToMessage(asset)}>
              <MessageSquarePlus size={16} />
            </IconAction>
          )}
          {extractMemory && (
            <IconAction
              label={memoryDisabled ? "Memory saving is disabled" : `Extract memory from ${asset.filename}`}
              onClick={() => extractMemory(asset)}
              disabled={memoryDisabled || Boolean(memoryBusy[asset.id])}
            >
              <Brain size={16} />
            </IconAction>
          )}
          <IconAction label={`Preview ${asset.filename}`} onClick={() => preview(asset)}>
            <AssetPreviewIcon asset={asset} />
          </IconAction>
          <IconAction label={`Download ${asset.filename}`} onClick={() => download(asset.id)}>
            <Download size={16} />
          </IconAction>
          <IconAction label={`Delete ${asset.filename}`} onClick={() => remove(asset.id)} danger>
            <Trash2 size={16} />
          </IconAction>
        </div>
      ))}
    </div>
  );
}

function AssetRowMain({
  asset,
  icon,
  openAsset,
  formatBytes,
  formatTime
}: {
  asset: Asset;
  icon: ReactNode;
  openAsset?: (asset: Asset) => void;
  formatBytes: (bytes: number) => string;
  formatTime: (value?: string) => string;
}) {
  const content = (
    <>
      {icon}
      <span>
        <strong>{asset.filename}</strong>
        <small>{formatBytes(asset.size_bytes)} · {formatTime(asset.created_at)}</small>
      </span>
    </>
  );
  if (!openAsset) return <div className="asset-row-main">{content}</div>;
  return (
    <button className="asset-row-main" type="button" onClick={() => openAsset(asset)}>
      {content}
    </button>
  );
}

function IconAction({
  label,
  onClick,
  disabled,
  danger,
  children
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          className={`icon ${danger ? "danger" : ""}`}
          onClick={(event) => {
            event.stopPropagation();
            onClick();
          }}
          disabled={disabled}
          aria-label={label}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function AssetPreviewIcon({ asset }: { asset: Asset }) {
  if (isTextAsset(asset) || isPDFAsset(asset)) return <FileText size={16} />;
  return <Image size={16} />;
}

function isPDFAsset(asset: Asset): boolean {
  return asset.content_type === "application/pdf" || asset.filename.toLowerCase().endsWith(".pdf");
}

function isTextAsset(asset: Asset): boolean {
  const contentType = asset.content_type || "";
  const filename = asset.filename.toLowerCase();
  return contentType.startsWith("text/") || contentType.includes("json") || [".md", ".txt", ".csv", ".json", ".log"].some((suffix) => filename.endsWith(suffix));
}
