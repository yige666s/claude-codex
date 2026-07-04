import { ReactNode } from "react";
import { Briefcase, FileUp, Image, Plug, Search, Sparkles, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";
import type { Asset, ConnectorPolicy, ConnectorStatus, Job, JobEvent, Skill } from "../../../types";
import type { JobStreamStatus, RightPanelTab } from "../workspaceTypes";
import { ApplicationPanel } from "./right-panel/ApplicationPanel";
import { AssetPanel } from "./right-panel/AssetPanel";
import { JobPanel } from "./right-panel/JobPanel";
import { SkillPanel } from "./right-panel/SkillPanel";

type WorkspaceResourceDialogProps = {
  open: boolean;
  activeTab: RightPanelTab;
  searchValue: string;
  visibleCount: number;
  totalCount: number;
  skills: Skill[];
  recentSkillNames: string[];
  jobs: Job[];
  applications: ConnectorStatus[];
  applicationBusy: string;
  applicationNotice?: string;
  selectedJobId: string;
  jobEvents: JobEvent[];
  jobStreamNotice: string;
  jobStreamStatus: JobStreamStatus;
  attachments: Asset[];
  artifacts: Asset[];
  uploadProgress: number;
  assetMemoryBusy: Record<string, boolean>;
  memoryDisabled: boolean;
  resourceNotices: Record<RightPanelTab, boolean>;
  onOpenChange: (open: boolean) => void;
  onTabChange: (tab: RightPanelTab) => void;
  onSearchChange: (value: string) => void;
  onLoadMore: () => void;
  onInsertSkill: (skill: Skill) => void;
  onSkillDetails: (skill: Skill) => void;
  onConnectApplication: (provider: string) => void;
  onUpdateApplicationPolicy: (provider: string, policy: ConnectorPolicy) => void;
  onDisconnectApplication: (provider: string) => void;
  onToggleJob: (jobId: string) => void;
  onCancelJob: () => void;
  onPreviewAttachment: (asset: Asset) => void;
  onDownloadAttachment: (id: string) => void;
  onDeleteAttachment: (id: string) => void;
  onAddAttachmentToMessage: (asset: Asset) => void;
  onPreviewArtifact: (asset: Asset) => void;
  onOpenArtifact: (asset: Asset) => void;
  onDownloadArtifact: (id: string) => void;
  onDeleteArtifact: (id: string) => void;
  onExtractMemory: (asset: Asset) => void;
  formatBytes: (bytes: number) => string;
  formatTime: (value?: string) => string;
};

const resourceTabs: Array<{ tab: RightPanelTab; label: string; icon: ReactNode }> = [
  { tab: "skills", label: "Skills", icon: <Sparkles size={17} /> },
  { tab: "jobs", label: "Jobs", icon: <Briefcase size={17} /> },
  { tab: "attachments", label: "Attachments", icon: <FileUp size={17} /> },
  { tab: "artifacts", label: "Artifacts", icon: <Image size={17} /> },
  { tab: "applications", label: "Applications", icon: <Plug size={17} /> }
];

export function WorkspaceResourceDialog({
  open,
  activeTab,
  searchValue,
  visibleCount,
  totalCount,
  skills,
  recentSkillNames,
  jobs,
  applications,
  applicationBusy,
  applicationNotice,
  selectedJobId,
  jobEvents,
  jobStreamNotice,
  jobStreamStatus,
  attachments,
  artifacts,
  uploadProgress,
  assetMemoryBusy,
  memoryDisabled,
  resourceNotices,
  onOpenChange,
  onTabChange,
  onSearchChange,
  onLoadMore,
  onInsertSkill,
  onSkillDetails,
  onConnectApplication,
  onUpdateApplicationPolicy,
  onDisconnectApplication,
  onToggleJob,
  onCancelJob,
  onPreviewAttachment,
  onDownloadAttachment,
  onDeleteAttachment,
  onAddAttachmentToMessage,
  onPreviewArtifact,
  onOpenArtifact,
  onDownloadArtifact,
  onDeleteArtifact,
  onExtractMemory,
  formatBytes,
  formatTime
}: WorkspaceResourceDialogProps) {
  const title = resourceTabs.find((item) => item.tab === activeTab)?.label || "Resources";
  const canLoadMore = visibleCount < totalCount;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="resource-modal" hideClose>
        <DialogTitle className="sr-only">{title}</DialogTitle>
        <DialogDescription className="sr-only">Search and browse workspace resources.</DialogDescription>
        <div className="resource-modal-head">
          <div className="resource-search-input">
            <Search size={18} />
            <Input
              value={searchValue}
              onChange={(event) => onSearchChange(event.target.value)}
              placeholder={`Search ${title.toLowerCase()}`}
              aria-label={`Search ${title.toLowerCase()}`}
              autoFocus
            />
          </div>
          <Button variant="ghost" size="icon" onClick={() => onOpenChange(false)} title="Close resources" aria-label="Close resources">
            <X size={20} />
          </Button>
        </div>
        <div className="resource-modal-tabs" role="tablist" aria-label="Resources">
          {resourceTabs.map((item) => (
            <Button
              key={item.tab}
              className={`resource-modal-tab ${item.tab === activeTab ? "active" : ""} ${resourceNotices[item.tab] ? "has-new" : ""}`}
              variant="ghost"
              role="tab"
              aria-selected={item.tab === activeTab}
              aria-label={resourceNotices[item.tab] ? `${item.label}, new item available` : item.label}
              onClick={() => onTabChange(item.tab)}
            >
              {item.icon}
              <span>{item.label}</span>
              {resourceNotices[item.tab] && <span className="resource-new-indicator" aria-hidden="true" />}
            </Button>
          ))}
        </div>
        <div
          className="resource-modal-body"
          onScroll={(event) => {
            const node = event.currentTarget;
            if (!canLoadMore) return;
            if (node.scrollTop + node.clientHeight >= node.scrollHeight - 80) onLoadMore();
          }}
        >
          {activeTab === "skills" && (
            <SkillPanel
              skills={skills}
              recentSkillNames={recentSkillNames}
              emptyLabel={searchValue ? "No matching skills" : "No skills"}
              onInsert={onInsertSkill}
              onDetails={onSkillDetails}
            />
          )}
          {activeTab === "jobs" && (
            <JobPanel
              jobs={jobs}
              selectedJobId={selectedJobId}
              jobEvents={jobEvents}
              jobStreamNotice={jobStreamNotice}
              jobStreamStatus={jobStreamStatus}
              emptyLabel={searchValue ? "No results" : "No items"}
              onToggleJob={onToggleJob}
              onCancelJob={onCancelJob}
              formatTime={formatTime}
            />
          )}
          {activeTab === "applications" && (
            <ApplicationPanel
              connectors={applications}
              busyProvider={applicationBusy}
              notice={applicationNotice}
              emptyLabel={searchValue ? "No matching applications" : "No applications"}
              onConnect={onConnectApplication}
              onPolicyChange={onUpdateApplicationPolicy}
              onDisconnect={onDisconnectApplication}
            />
          )}
          {activeTab === "attachments" && (
            <AssetPanel
              assets={attachments}
              icon="file"
              emptyLabel={searchValue ? "No results" : "No items"}
              uploadProgress={uploadProgress}
              preview={onPreviewAttachment}
              download={onDownloadAttachment}
              remove={onDeleteAttachment}
              extractMemory={onExtractMemory}
              memoryBusy={assetMemoryBusy}
              memoryDisabled={memoryDisabled}
              addToMessage={onAddAttachmentToMessage}
              formatBytes={formatBytes}
              formatTime={formatTime}
            />
          )}
          {activeTab === "artifacts" && (
            <AssetPanel
              assets={artifacts}
              icon="image"
              emptyLabel={searchValue ? "No results" : "No items"}
              openAsset={onOpenArtifact}
              preview={onPreviewArtifact}
              download={onDownloadArtifact}
              remove={onDeleteArtifact}
              extractMemory={onExtractMemory}
              memoryBusy={assetMemoryBusy}
              memoryDisabled={memoryDisabled}
              formatBytes={formatBytes}
              formatTime={formatTime}
            />
          )}
          {canLoadMore && (
            <Button className="resource-load-more" variant="ghost" onClick={onLoadMore}>
              Load 10 more
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
