import { Globe2, Image, Lightbulb, type LucideIcon } from "lucide-react";
import type { ComposerToolID } from "../../workspaceTypes";

type ComposerToolChipsProps = {
  selectedToolId: ComposerToolID | "";
  disabled: boolean;
  onSelectTool: (toolId: ComposerToolID) => void;
};

const composerTools: Array<{
  id: ComposerToolID;
  label: string;
  ariaLabel: string;
  icon: LucideIcon;
}> = [
  { id: "image", label: "生成图片", ariaLabel: "Use image generation", icon: Image },
  { id: "thinking", label: "思考一下", ariaLabel: "Use model thinking", icon: Lightbulb },
  { id: "web-search", label: "查找资料", ariaLabel: "Use web search", icon: Globe2 }
];

export function ComposerToolChips({ selectedToolId, disabled, onSelectTool }: ComposerToolChipsProps) {
  return (
    <div className="composer-tool-row" aria-label="Composer tools">
      {composerTools.map((tool) => {
        const Icon = tool.icon;
        const active = selectedToolId === tool.id;
        return (
          <button
            key={tool.id}
            type="button"
            className={`composer-tool-chip ${active ? "active" : ""}`}
            aria-label={tool.ariaLabel}
            aria-pressed={active}
            disabled={disabled}
            onClick={() => onSelectTool(tool.id)}
          >
            <Icon size={17} />
            <span>{tool.label}</span>
          </button>
        );
      })}
    </div>
  );
}
