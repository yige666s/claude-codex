import { ReactNode } from "react";
import { FileUp, Image, PanelLeft, Search, Sparkles, Briefcase, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { ScrollArea } from "../../../components/ui/scroll-area";
import { Tabs, TabsList, TabsTrigger } from "../../../components/ui/tabs";
import { Badge } from "../../../components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "../../../components/ui/tooltip";
import { MotionPanel } from "../../../components/motion";
import type { RightPanelTab } from "../workspaceTypes";

type WorkspaceToolsPanelProps = {
  open: boolean;
  activeTab: RightPanelTab;
  searchValue: string;
  counts: Record<RightPanelTab, number>;
  children: ReactNode;
  onToggle: () => void;
  onTabChange: (tab: RightPanelTab) => void;
  onSearchChange: (value: string) => void;
};

const tabs: Array<{ tab: RightPanelTab; label: string; icon: ReactNode }> = [
  { tab: "skills", label: "Skills", icon: <Sparkles size={20} /> },
  { tab: "jobs", label: "Jobs", icon: <Briefcase size={20} /> },
  { tab: "attachments", label: "Attachments", icon: <FileUp size={20} /> },
  { tab: "artifacts", label: "Artifacts", icon: <Image size={20} /> }
];

export function WorkspaceToolsPanel({
  open,
  activeTab,
  searchValue,
  counts,
  children,
  onToggle,
  onTabChange,
  onSearchChange
}: WorkspaceToolsPanelProps) {
  return (
    <>
      <Button
        className="right-panel-toggle"
        variant="outline"
        size="icon-sm"
        onClick={onToggle}
        aria-label={open ? "Collapse right panel" : "Expand right panel"}
        title={open ? "Collapse right panel" : "Expand right panel"}
        aria-expanded={open}
      >
        <PanelLeft size={18} />
      </Button>

      <aside className="right-panel" aria-hidden={!open}>
        <Tabs value={activeTab} onValueChange={(value) => onTabChange(value as RightPanelTab)}>
          <TabsList className="right-tabs" aria-label="Right panel tools">
            {tabs.map((item) => (
              <TabsTrigger
                key={item.tab}
                className="right-tab"
                value={item.tab}
                title={item.label}
                aria-label={item.label}
              >
                {item.icon}
                <span className="sr-only">{item.label}</span>
                <Badge className="tab-count" variant="secondary">{counts[item.tab]}</Badge>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <div className="right-search">
          <Search size={16} />
          <Input
            value={searchValue}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={`Search ${rightPanelLabel(activeTab)}`}
            aria-label={`Search ${rightPanelLabel(activeTab)}`}
          />
          {searchValue && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button className="icon" variant="ghost" size="icon-sm" onClick={() => onSearchChange("")} aria-label="Clear search">
                  <X size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Clear search</TooltipContent>
            </Tooltip>
          )}
        </div>
        <ScrollArea className="right-tab-content">
          <MotionPanel key={activeTab} className="right-panel-motion">
            {children}
          </MotionPanel>
        </ScrollArea>
      </aside>
    </>
  );
}

function rightPanelLabel(tab: RightPanelTab): string {
  switch (tab) {
    case "skills":
      return "skills";
    case "jobs":
      return "jobs";
    case "attachments":
      return "attachments";
    case "artifacts":
      return "artifacts";
  }
}
