import { Ref, RefObject, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, FileText, FileUp, Image, Library, Paperclip, Search, X } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/input";
import type { Asset } from "../../../../types";

type ComposerUploadButtonProps = {
  inputRef: RefObject<HTMLInputElement | null>;
  uploading: boolean;
  disabled: boolean;
  accept: string;
  attachments: Asset[];
  placement: "above" | "below";
  onUpload: (files: FileList | null) => void;
  onAddExistingAttachment: (asset: Asset) => void;
  formatTime: (value?: string) => string;
};

export function ComposerUploadButton({
  inputRef,
  uploading,
  disabled,
  accept,
  attachments,
  placement,
  onUpload,
  onAddExistingAttachment,
  formatTime
}: ComposerUploadButtonProps) {
  const [open, setOpen] = useState(false);
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [libraryQuery, setLibraryQuery] = useState("");
  const [selectedAssetIds, setSelectedAssetIds] = useState<string[]>([]);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const sortedAttachments = useMemo(() => (
    [...attachments]
      .sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime())
  ), [attachments]);
  const recentAttachments = useMemo(() => sortedAttachments.slice(0, 5), [sortedAttachments]);
  const filteredAttachments = useMemo(() => {
    const query = libraryQuery.trim().toLowerCase();
    if (!query) return sortedAttachments;
    return sortedAttachments.filter((asset) => (
      asset.filename.toLowerCase().includes(query)
      || asset.content_type.toLowerCase().includes(query)
    ));
  }, [libraryQuery, sortedAttachments]);

  useEffect(() => {
    if (!open) return;
    function handlePointerDown(event: MouseEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  const triggerUpload = () => {
    setOpen(false);
    inputRef.current?.click();
  };

  const openLibrary = () => {
    setOpen(false);
    setLibraryOpen(true);
  };

  const closeLibrary = () => {
    setLibraryOpen(false);
    setLibraryQuery("");
    setSelectedAssetIds([]);
  };

  const toggleSelectedAsset = (assetId: string) => {
    setSelectedAssetIds((current) => (
      current.includes(assetId)
        ? current.filter((id) => id !== assetId)
        : [...current, assetId]
    ));
  };

  const addSelectedAssets = () => {
    const selected = sortedAttachments.filter((asset) => selectedAssetIds.includes(asset.id));
    selected.forEach(onAddExistingAttachment);
    closeLibrary();
  };

  const libraryDialog = libraryOpen ? (
    <div className="composer-attachment-library-backdrop" role="presentation">
      <div className="composer-attachment-library-dialog" role="dialog" aria-modal="true" aria-label="从库中添加">
        <header className="composer-attachment-library-head">
          <div>
            <h2>从库中添加</h2>
            <p>选择已有附件附加到本次聊天</p>
          </div>
          <button type="button" className="composer-attachment-library-close" aria-label="关闭附件库" onClick={closeLibrary}>
            <X size={20} />
          </button>
        </header>
        <label className="composer-attachment-library-search">
          <Search size={18} />
          <input
            value={libraryQuery}
            aria-label="搜索附件库"
            placeholder="搜索库"
            onChange={(event) => setLibraryQuery(event.target.value)}
          />
        </label>
        <div className="composer-attachment-library-list" role="listbox" aria-label="附件库" aria-multiselectable="true">
          {filteredAttachments.length > 0 ? filteredAttachments.map((asset) => {
            const selected = selectedAssetIds.includes(asset.id);
            return (
              <button
                key={asset.id}
                type="button"
                className={`composer-attachment-library-option${selected ? " selected" : ""}`}
                role="option"
                aria-selected={selected}
                onClick={() => toggleSelectedAsset(asset.id)}
              >
                <span className="composer-attachment-file-icon">
                  {asset.content_type.startsWith("image/") ? <Image size={18} /> : <FileText size={18} />}
                </span>
                <span className="composer-attachment-file-copy">
                  <span>{asset.filename}</span>
                  <small>{formatTime(asset.created_at)} · {asset.content_type}</small>
                </span>
                <span className="composer-attachment-library-check" aria-hidden="true">
                  {selected ? <Check size={16} /> : null}
                </span>
              </button>
            );
          }) : (
            <div className="composer-attachment-empty">没有匹配的附件</div>
          )}
        </div>
        <footer className="composer-attachment-library-footer">
          <span>已选 {selectedAssetIds.length} 个</span>
          <div className="composer-attachment-library-actions">
            <button type="button" className="composer-attachment-library-cancel" onClick={closeLibrary}>取消</button>
            <button type="button" className="composer-attachment-library-add" onClick={addSelectedAssets} disabled={selectedAssetIds.length === 0}>
              添加至聊天
            </button>
          </div>
        </footer>
      </div>
    </div>
  ) : null;

  return (
    <div className="composer-upload-wrap" ref={rootRef}>
      <Button
        type="button"
        className="composer-upload"
        variant="outline"
        size="icon-lg"
        title="Add attachment"
        aria-label="Add attachment"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        disabled={uploading || disabled}
      >
        <Paperclip size={18} />
      </Button>
      {open && (
        <div className={`composer-attachment-menu placement-${placement}`} role="menu" aria-label="Add attachment">
          <button type="button" className="composer-attachment-menu-item" role="menuitem" onClick={triggerUpload}>
            <span className="composer-attachment-menu-icon"><FileUp size={19} /></span>
            <span>上传照片和文件</span>
          </button>
          <button type="button" className="composer-attachment-library-title" role="menuitem" onClick={openLibrary}>
            <span>
              <span className="composer-attachment-menu-icon"><Library size={18} /></span>
              <span>从库中添加</span>
            </span>
          </button>
          <div className="composer-attachment-library">
            <div className="composer-attachment-library-label">最近</div>
            {recentAttachments.length > 0 ? (
              <div className="composer-attachment-recent-list">
                {recentAttachments.map((asset) => (
                  <button
                    key={asset.id}
                    type="button"
                    className="composer-attachment-file"
                    role="menuitem"
                    onClick={() => {
                      onAddExistingAttachment(asset);
                      setOpen(false);
                    }}
                  >
                    <span className="composer-attachment-file-icon">
                      {asset.content_type.startsWith("image/") ? <Image size={18} /> : <FileText size={18} />}
                    </span>
                    <span className="composer-attachment-file-copy">
                      <span>{asset.filename}</span>
                      <small>{formatTime(asset.created_at)}</small>
                    </span>
                  </button>
                ))}
              </div>
            ) : (
              <div className="composer-attachment-empty">暂无可选附件</div>
            )}
          </div>
        </div>
      )}
      {libraryDialog ? createPortal(libraryDialog, document.body) : null}
      <Input
        ref={inputRef as Ref<HTMLInputElement>}
        className="composer-file-input"
        type="file"
        tabIndex={-1}
        aria-hidden="true"
        accept={accept}
        onChange={(event) => {
          onUpload(event.currentTarget.files);
          event.currentTarget.value = "";
        }}
        disabled={uploading || disabled}
      />
    </div>
  );
}
