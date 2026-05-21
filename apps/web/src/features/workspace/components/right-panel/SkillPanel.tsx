import { forwardRef, useMemo, useRef, useState } from "react";
import { Archive, ChevronDown, Clock, Info, PlayCircle, Sparkles, Star } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import type { Skill } from "../../../../types";

type SkillPanelProps = {
  skills: Skill[];
  recentSkillNames: string[];
  emptyLabel: string;
  onInsert: (skill: Skill) => void;
  onDetails: (skill: Skill) => void;
};

export function SkillPanel({
  skills,
  recentSkillNames,
  emptyLabel,
  onInsert,
  onDetails
}: SkillPanelProps) {
  const recentSkills = recentSkillsFromNames(skills, recentSkillNames);
  const orderedSkills = useMemo(() => [...skills].sort(compareSkills), [skills]);
  const [expandedSkillName, setExpandedSkillName] = useState("");
  const skillRefs = useRef(new Map<string, HTMLElement>());
  if (!orderedSkills.length) return <div className="empty-small">{emptyLabel}</div>;
  const jumpToSkill = (skill: Skill) => {
    setExpandedSkillName(skill.name);
    window.requestAnimationFrame(() => {
      skillRefs.current.get(skill.name)?.scrollIntoView({ block: "start", behavior: "smooth" });
    });
  };
  const setSkillRef = (name: string) => (element: HTMLElement | null) => {
    if (element) skillRefs.current.set(name, element);
    else skillRefs.current.delete(name);
  };
  return (
    <div className="skill-browser">
      {recentSkills.length > 0 && (
        <section className="skill-section">
          <div className="skill-section-title">
            <Star size={14} />
            <span>Recent</span>
            <small>{recentSkills.length}</small>
          </div>
          <div className="recent-skill-list">
            {recentSkills.map((skill) => (
              <Button key={skill.name} type="button" onClick={() => jumpToSkill(skill)}>
                <SkillGlyph skill={skill} />
                <span>{skill.display_name || skill.name}</span>
              </Button>
            ))}
          </div>
        </section>
      )}
      <section className="skill-section">
        <div className="skill-section-title">
          <Sparkles size={14} />
          <span>Skills</span>
          <small>{orderedSkills.length}</small>
        </div>
        <div className="skill-grid">
          {orderedSkills.map((skill) => {
            const expanded = expandedSkillName === skill.name;
            return (
              <SkillCard
                key={skill.name}
                ref={setSkillRef(skill.name)}
                skill={skill}
                expanded={expanded}
                onToggle={() => setExpandedSkillName(expanded ? "" : skill.name)}
                onInsert={onInsert}
                onDetails={onDetails}
              />
            );
          })}
        </div>
      </section>
    </div>
  );
}

const SkillCard = forwardRef<HTMLElement, {
  skill: Skill;
  expanded: boolean;
  onToggle: () => void;
  onInsert: (skill: Skill) => void;
  onDetails: (skill: Skill) => void;
}>(function SkillCard({
  skill,
  expanded,
  onToggle,
  onInsert,
  onDetails
}, ref) {
  const title = skill.display_name || skill.name;
  return (
    <article ref={ref} className={`skill-card ${expanded ? "expanded" : "collapsed"} ${skill.featured ? "featured" : ""}`}>
      <Button type="button" className="skill-card-summary" onClick={onToggle} aria-expanded={expanded}>
        <SkillGlyph skill={skill} />
        <div>
          <strong>{title}</strong>
          <small>/{skill.name}</small>
        </div>
        <ChevronDown size={16} />
      </Button>
      {expanded && (
        <>
          <p>{skill.short_description || skill.description || skill.usage || "No description available."}</p>
          <div className="skill-meta-row">
            {skill.run_as_job && <span>Job</span>}
            {skill.produces_artifacts && <span>Artifacts</span>}
            {skill.version && <span>v{skill.version}</span>}
          </div>
          <div className="skill-card-actions">
            <Button type="button" className="skill-action primary" onClick={() => onInsert(skill)} title={`Apply /${skill.name}`} aria-label={`Apply /${skill.name}`}>
              <PlayCircle size={15} />
              <span>Apply</span>
            </Button>
            <Button type="button" className="skill-action" onClick={() => onDetails(skill)} title={`Details for ${title}`} aria-label="Skill details">
              <Info size={15} />
            </Button>
          </div>
        </>
      )}
    </article>
  );
});

export function SkillGlyph({ skill }: { skill: Skill }) {
  if (skill.icon) return <span className="skill-glyph text">{skill.icon}</span>;
  if (skill.produces_artifacts) return <span className="skill-glyph"><Archive size={17} /></span>;
  if (skill.run_as_job) return <span className="skill-glyph"><Clock size={17} /></span>;
  return <span className="skill-glyph"><Sparkles size={17} /></span>;
}

function compareSkills(a: Skill, b: Skill): number {
  if (Boolean(a.featured) !== Boolean(b.featured)) return a.featured ? -1 : 1;
  const orderA = a.sort_order ?? Number.MAX_SAFE_INTEGER;
  const orderB = b.sort_order ?? Number.MAX_SAFE_INTEGER;
  if (orderA !== orderB) return orderA - orderB;
  return (a.display_name || a.name).localeCompare(b.display_name || b.name);
}

function recentSkillsFromNames(skills: Skill[], names: string[]): Skill[] {
  const byName = new Map(skills.map((skill) => [skill.name, skill]));
  const seenNames = new Set<string>();
  const out: Skill[] = [];
  for (const name of names) {
    const skill = byName.get(name);
    if (!skill || seenNames.has(skill.name)) continue;
    seenNames.add(skill.name);
    out.push(skill);
  }
  return out;
}
