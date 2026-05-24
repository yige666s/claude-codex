import { Archive, Bot, Check, ChevronDown, Clock, Mic, Sparkles } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "../../../../components/ui/dropdown-menu";
import type { Skill } from "../../../../types";
import type { InputMode } from "../../hooks/useLiveVoice";

type ToolModeSelectorProps = {
  inputMode: InputMode;
  skills: Skill[];
  recentSkillNames: string[];
  selectedSkillName: string;
  busyChat: boolean;
  sessionId: string;
  onSelectChat: () => void;
  onSelectLive: () => void;
  onSelectSkill: (skill: Skill) => void;
};

export function ToolModeSelector({
  inputMode,
  skills,
  recentSkillNames,
  selectedSkillName,
  busyChat,
  sessionId,
  onSelectChat,
  onSelectLive,
  onSelectSkill
}: ToolModeSelectorProps) {
  const selectedSkill = skills.find((skill) => skill.name === selectedSkillName);
  const orderedSkills = orderSkills(skills, recentSkillNames);
  const activeKind = inputMode === "live" ? "live" : selectedSkill ? "skill" : "chat";
  const label = activeKind === "live" ? "Live" : selectedSkill ? skillLabel(selectedSkill) : "Chat";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          className={`tool-mode-trigger ${activeKind}`}
          disabled={busyChat}
          title="Choose mode or tool"
          aria-label="Choose mode or tool"
        >
          {activeKind === "live" ? <Mic size={16} /> : selectedSkill ? <SkillModeIcon skill={selectedSkill} /> : <Bot size={16} />}
          <span>{label}</span>
          <ChevronDown size={14} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="tool-mode-menu" align="end" side="top" sideOffset={8}>
        <DropdownMenuLabel>Mode</DropdownMenuLabel>
        <DropdownMenuItem onClick={onSelectChat}>
          <Bot size={16} />
          <span>Chat</span>
          {activeKind === "chat" && <Check className="tool-mode-check" size={15} />}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onSelectLive} disabled={!sessionId || busyChat}>
          <Mic size={16} />
          <span>Live</span>
          {activeKind === "live" && <Check className="tool-mode-check" size={15} />}
        </DropdownMenuItem>
        {orderedSkills.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>Tools</DropdownMenuLabel>
            <div className="tool-mode-skill-list">
              {orderedSkills.map((skill) => (
                <DropdownMenuItem key={skill.name} onClick={() => onSelectSkill(skill)}>
                  <SkillModeIcon skill={skill} />
                  <span>
                    <strong>{skillLabel(skill)}</strong>
                    <small>/{skill.name}</small>
                  </span>
                  {selectedSkillName === skill.name && inputMode !== "live" && <Check className="tool-mode-check" size={15} />}
                </DropdownMenuItem>
              ))}
            </div>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SkillModeIcon({ skill }: { skill: Skill }) {
  if (skill.produces_artifacts) return <Archive size={16} />;
  if (skill.run_as_job) return <Clock size={16} />;
  return <Sparkles size={16} />;
}

function skillLabel(skill: Skill): string {
  return skill.display_name || skill.name;
}

function orderSkills(skills: Skill[], recentSkillNames: string[]): Skill[] {
  const recentIndex = new Map(recentSkillNames.map((name, index) => [name, index]));
  return [...skills].sort((a, b) => {
    const recentA = recentIndex.get(a.name);
    const recentB = recentIndex.get(b.name);
    if (recentA !== undefined || recentB !== undefined) {
      if (recentA === undefined) return 1;
      if (recentB === undefined) return -1;
      return recentA - recentB;
    }
    if (Boolean(a.featured) !== Boolean(b.featured)) return a.featured ? -1 : 1;
    const orderA = a.sort_order ?? Number.MAX_SAFE_INTEGER;
    const orderB = b.sort_order ?? Number.MAX_SAFE_INTEGER;
    if (orderA !== orderB) return orderA - orderB;
    return skillLabel(a).localeCompare(skillLabel(b));
  });
}
