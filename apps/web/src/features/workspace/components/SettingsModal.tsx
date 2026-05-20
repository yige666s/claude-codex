import { useEffect, useState } from "react";
import { Brain, Database, UserX, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";
import { Textarea } from "../../../components/ui/textarea";
import type { MemorySettings, PersonalizationSettings } from "../../../types";

const personalizationStyleOptions = [
  { value: "default", label: "Default" },
  { value: "professional_reliable", label: "Professional reliable" },
  { value: "friendly", label: "Friendly" },
  { value: "direct", label: "Direct" },
  { value: "imaginative", label: "Imaginative" },
  { value: "efficient", label: "Efficient" },
  { value: "witty", label: "Witty" }
];

const personalizationTraitOptions = [
  { value: "enhanced", label: "Enhanced" },
  { value: "default", label: "Default" },
  { value: "reduced", label: "Reduced" }
];
const personalizationTextLimits = {
  nickname: 120,
  occupation: 160,
  about: 2000,
  customInstructions: 4000
};

function normalizePersonalizationDraft(settings: PersonalizationSettings): PersonalizationSettings {
  return {
    ...settings,
    profile: {
      nickname: settings.profile.nickname || "",
      occupation: settings.profile.occupation || "",
      about: settings.profile.about || ""
    },
    custom_instructions: settings.custom_instructions || ""
  };
}

function personalizationPatchFromDraft(settings: PersonalizationSettings): Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">> {
  return {
    profile: settings.profile,
    style: settings.style,
    traits: settings.traits,
    custom_instructions: settings.custom_instructions,
    feature_flags: settings.feature_flags
  };
}

export function SettingsModal({
  userLabel,
  memorySettings,
  personalizationSettings,
  personalizationSaving,
  hasSession,
  onUpdateMemorySettings,
  onUpdatePersonalization,
  onResetPersonalization,
  onManageMemory,
  onDeleteSessionMemory,
  onDeleteAllMemory,
  onExportData,
  onDeleteAccount,
  onLogout,
  onClose
}: {
  userLabel: string;
  memorySettings: MemorySettings;
  personalizationSettings: PersonalizationSettings;
  personalizationSaving: boolean;
  hasSession: boolean;
  onUpdateMemorySettings: (patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) => void;
  onUpdatePersonalization: (patch: Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">>) => void;
  onResetPersonalization: () => void;
  onManageMemory: () => void;
  onDeleteSessionMemory: () => void;
  onDeleteAllMemory: () => void;
  onExportData: () => void;
  onDeleteAccount: () => void;
  onLogout: () => void;
  onClose: () => void;
}) {
  const [activeSection, setActiveSection] = useState<"personalization" | "data" | "account">("personalization");
  const [draftPersonalization, setDraftPersonalization] = useState<PersonalizationSettings>(personalizationSettings);

  useEffect(() => {
    setDraftPersonalization(personalizationSettings);
  }, [personalizationSettings]);

  const personalizationDirty = JSON.stringify(normalizePersonalizationDraft(draftPersonalization)) !== JSON.stringify(normalizePersonalizationDraft(personalizationSettings));

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onClose();
    }}>
      <DialogContent className="settings-modal" hideClose>
        <aside className="settings-nav" aria-label="Settings sections">
          <Button className="icon settings-close" onClick={onClose} title="Close settings" aria-label="Close settings">
            <X size={22} />
          </Button>
          <Button className={`settings-nav-item ${activeSection === "personalization" ? "active" : ""}`} onClick={() => setActiveSection("personalization")}><Brain size={18} /> Personalization</Button>
          <Button className={`settings-nav-item ${activeSection === "data" ? "active" : ""}`} onClick={() => setActiveSection("data")}><Database size={18} /> Data controls</Button>
          <Button className={`settings-nav-item ${activeSection === "account" ? "active" : ""}`} onClick={() => setActiveSection("account")}><UserX size={18} /> Account</Button>
        </aside>
        <section className="settings-panel">
          {activeSection === "personalization" && (
            <PersonalizationSettingsPanel
              userLabel={userLabel}
              draft={draftPersonalization}
              dirty={personalizationDirty}
              saving={personalizationSaving}
              memorySettings={memorySettings}
              onDraftChange={setDraftPersonalization}
              onSave={() => onUpdatePersonalization(personalizationPatchFromDraft(draftPersonalization))}
              onReset={onResetPersonalization}
              onManageMemory={onManageMemory}
            />
          )}
          {activeSection === "data" && (
            <DataControlsSettingsPanel
              userLabel={userLabel}
              memorySettings={memorySettings}
              hasSession={hasSession}
              onUpdateMemorySettings={onUpdateMemorySettings}
              onManageMemory={onManageMemory}
              onDeleteSessionMemory={onDeleteSessionMemory}
              onDeleteAllMemory={onDeleteAllMemory}
              onExportData={onExportData}
            />
          )}
          {activeSection === "account" && (
            <AccountSettingsPanel
              userLabel={userLabel}
              onLogout={onLogout}
              onDeleteAccount={onDeleteAccount}
            />
          )}
        </section>
      </DialogContent>
    </Dialog>
  );
}

function PersonalizationSettingsPanel({
  userLabel,
  draft,
  dirty,
  saving,
  memorySettings,
  onDraftChange,
  onSave,
  onReset,
  onManageMemory
}: {
  userLabel: string;
  draft: PersonalizationSettings;
  dirty: boolean;
  saving: boolean;
  memorySettings: MemorySettings;
  onDraftChange: (settings: PersonalizationSettings) => void;
  onSave: () => void;
  onReset: () => void;
  onManageMemory: () => void;
}) {
  const updateProfile = (patch: Partial<PersonalizationSettings["profile"]>) => {
    onDraftChange({ ...draft, profile: { ...draft.profile, ...patch } });
  };
  const updateStyle = (patch: Partial<PersonalizationSettings["style"]>) => {
    onDraftChange({ ...draft, style: { ...draft.style, ...patch } });
  };
  const updateTraits = (patch: Partial<PersonalizationSettings["traits"]>) => {
    onDraftChange({ ...draft, traits: { ...draft.traits, ...patch } });
  };
  const updateFlags = (patch: Partial<PersonalizationSettings["feature_flags"]>) => {
    onDraftChange({ ...draft, feature_flags: { ...draft.feature_flags, ...patch } });
  };
  const saveState = saving ? "Saving changes..." : dirty ? "Unsaved changes" : "All changes saved";

  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Personalization</h2>
          <small>{userLabel}</small>
        </div>
      </header>

      <div className="settings-section-title">
        <strong>Response behavior</strong>
        <p>Controls that are applied before saved memory and recent chat history.</p>
      </div>
      <div className="settings-row">
        <div>
          <strong>Basic style and tone</strong>
          <p>Choose the response style used before memory or conversation context is considered.</p>
        </div>
        <select className="settings-select" value={draft.style.preset} onChange={(event) => updateStyle({ preset: event.target.value })} aria-label="Basic style">
          {personalizationStyleOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </div>
      <div className="settings-row">
        <div>
          <strong>Tone</strong>
          <p>Fine-tune the overall speaking tone independently from the base style.</p>
        </div>
        <select className="settings-select" value={draft.style.tone} onChange={(event) => updateStyle({ tone: event.target.value })} aria-label="Tone">
          {personalizationStyleOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </div>
      <div className="settings-fieldset">
        <div>
          <strong>Traits</strong>
          <p>Adjust optional behavior layered on top of the selected style.</p>
        </div>
        <div className="settings-trait-grid">
          <LabeledSelect label="Warmth" value={draft.traits.warmth} options={personalizationTraitOptions} onChange={(value) => updateTraits({ warmth: value })} />
          <LabeledSelect label="Enthusiasm" value={draft.traits.enthusiasm} options={personalizationTraitOptions} onChange={(value) => updateTraits({ enthusiasm: value })} />
          <LabeledSelect label="Headings and lists" value={draft.traits.headings_and_lists} options={personalizationTraitOptions} onChange={(value) => updateTraits({ headings_and_lists: value })} />
          <LabeledSelect label="Emoji" value={draft.traits.emoji} options={personalizationTraitOptions} onChange={(value) => updateTraits({ emoji: value })} />
        </div>
      </div>
      <div className="settings-row">
        <div>
          <strong>Quick answers</strong>
          <p>Simple questions get direct, compact answers; complex work still uses normal depth.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.quick_answers} label="Quick answers" onChange={(checked) => updateFlags({ quick_answers: checked })} />
      </div>
      <div className="settings-section-title">
        <strong>Instructions</strong>
        <p>Explicit instructions override saved memory when they conflict.</p>
      </div>
      <div className="settings-fieldset">
        <div className="settings-label-row">
          <label className="settings-input-label" htmlFor="custom-instructions">Custom instructions</label>
          <CharCounter value={draft.custom_instructions} max={personalizationTextLimits.customInstructions} />
        </div>
        <Textarea
          id="custom-instructions"
          className="settings-textarea"
          maxLength={personalizationTextLimits.customInstructions}
          value={draft.custom_instructions}
          onChange={(event) => onDraftChange({ ...draft, custom_instructions: event.target.value })}
          placeholder="Example: Reply in Chinese unless I ask otherwise."
        />
      </div>
      <div className="settings-section-title">
        <strong>About you</strong>
        <p>Stable profile details that should be available across conversations.</p>
      </div>
      <div className="settings-fieldset">
        <div className="settings-input-grid">
          <label className="settings-input-label">
            <span className="settings-label-row"><span>Nickname</span><CharCounter value={draft.profile.nickname || ""} max={personalizationTextLimits.nickname} /></span>
            <Input className="settings-input" maxLength={personalizationTextLimits.nickname} value={draft.profile.nickname || ""} onChange={(event) => updateProfile({ nickname: event.target.value })} placeholder="What should Agent call you?" />
          </label>
          <label className="settings-input-label">
            <span className="settings-label-row"><span>Occupation</span><CharCounter value={draft.profile.occupation || ""} max={personalizationTextLimits.occupation} /></span>
            <Input className="settings-input" maxLength={personalizationTextLimits.occupation} value={draft.profile.occupation || ""} onChange={(event) => updateProfile({ occupation: event.target.value })} placeholder="Product manager, engineer..." />
          </label>
        </div>
        <div className="settings-label-row">
          <label className="settings-input-label" htmlFor="about-you">Details</label>
          <CharCounter value={draft.profile.about || ""} max={personalizationTextLimits.about} />
        </div>
        <Textarea
          id="about-you"
          className="settings-textarea compact"
          maxLength={personalizationTextLimits.about}
          value={draft.profile.about || ""}
          onChange={(event) => updateProfile({ about: event.target.value })}
          placeholder="Background, preferred depth, domain context, or recurring constraints."
        />
      </div>
      <div className="settings-section-title">
        <strong>Context sources</strong>
        <p>Choose which stored or external context may be referenced when answering.</p>
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference saved memory</strong>
          <p>{memorySettings.context_enabled ? "Use curated saved memory when building replies." : "Memory context is disabled in Data controls."}</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_saved_memory} label="Reference saved memory" onChange={(checked) => updateFlags({ use_saved_memory: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference recent chats</strong>
          <p>Include recent visible conversation history in model context.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_chat_history} label="Reference recent chats" onChange={(checked) => updateFlags({ use_chat_history: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Reference browser memory</strong>
          <p>Use browser or external context submitted through the browser memory API.</p>
        </div>
        <SwitchButton checked={draft.feature_flags.use_browser_memory} label="Reference browser memory" onChange={(checked) => updateFlags({ use_browser_memory: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Saved memory</strong>
          <p>Review what the automatic memory system currently stores.</p>
        </div>
        <Button className="settings-action" onClick={onManageMemory}>Manage</Button>
      </div>
      <div className="settings-footer-actions">
        <span className={`settings-save-state ${dirty ? "dirty" : ""}`}>{saveState}</span>
        <Button className="settings-action" onClick={onReset} disabled={saving}>Reset</Button>
        <Button className="settings-action primary" onClick={onSave} disabled={!dirty || saving}>{saving ? "Saving" : "Save"}</Button>
      </div>
    </>
  );
}

function CharCounter({ value, max }: { value: string; max: number }) {
  const length = Array.from(value || "").length;
  return <span className="settings-char-count">{length}/{max}</span>;
}

function DataControlsSettingsPanel({
  userLabel,
  memorySettings,
  hasSession,
  onUpdateMemorySettings,
  onManageMemory,
  onDeleteSessionMemory,
  onDeleteAllMemory,
  onExportData
}: {
  userLabel: string;
  memorySettings: MemorySettings;
  hasSession: boolean;
  onUpdateMemorySettings: (patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) => void;
  onManageMemory: () => void;
  onDeleteSessionMemory: () => void;
  onDeleteAllMemory: () => void;
  onExportData: () => void;
}) {
  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Data controls</h2>
          <small>{userLabel}</small>
        </div>
      </header>

      <div className="settings-row">
        <div>
          <strong>Memory</strong>
          <p>Let AgentAPI save useful preferences and project context. Sensitive values are redacted before saving.</p>
        </div>
        <SwitchButton checked={memorySettings.enabled} label="Enable memory" onChange={(checked) => onUpdateMemorySettings({ enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Save new memory</strong>
          <p>Allow new chats, attachments, and artifacts to create memory.</p>
        </div>
        <SwitchButton checked={memorySettings.capture_enabled} label="Save new memory" onChange={(checked) => onUpdateMemorySettings({ capture_enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Use saved memory</strong>
          <p>Allow saved memory to be included in future model context.</p>
        </div>
        <SwitchButton checked={memorySettings.context_enabled} label="Use saved memory" onChange={(checked) => onUpdateMemorySettings({ context_enabled: checked })} />
      </div>
      <div className="settings-row">
        <div>
          <strong>Saved memory</strong>
          <p>Review, edit, delete, and resolve saved memory items.</p>
        </div>
        <Button className="settings-action" onClick={onManageMemory}>Manage</Button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Current session memory</strong>
          <p>Remove memory saved from the current session only.</p>
        </div>
        <Button className="settings-action" onClick={onDeleteSessionMemory} disabled={!hasSession}>Delete</Button>
      </div>
      <div className="settings-row">
        <div>
          <strong>All memory</strong>
          <p>Remove all saved memory for this account.</p>
        </div>
        <Button className="settings-action danger-outline" onClick={onDeleteAllMemory}>Delete all</Button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Export data</strong>
          <p>Download account data, sessions, artifacts, jobs, and memory.</p>
        </div>
        <Button className="settings-action" onClick={onExportData}>Export</Button>
      </div>
    </>
  );
}

function AccountSettingsPanel({ userLabel, onLogout, onDeleteAccount }: { userLabel: string; onLogout: () => void; onDeleteAccount: () => void }) {
  return (
    <>
      <header>
        <div>
          <h2 id="settings-title">Account</h2>
          <small>{userLabel}</small>
        </div>
      </header>
      <div className="settings-row">
        <div>
          <strong>Session</strong>
          <p>Sign out of this browser.</p>
        </div>
        <Button className="settings-action" onClick={onLogout}>Log out</Button>
      </div>
      <div className="settings-row">
        <div>
          <strong>Delete account</strong>
          <p>Permanently remove this account and associated data.</p>
        </div>
        <Button className="settings-action danger-outline" onClick={onDeleteAccount}>Delete</Button>
      </div>
    </>
  );
}

function LabeledSelect({ label, value, options, onChange }: { label: string; value: string; options: Array<{ value: string; label: string }>; onChange: (value: string) => void }) {
  return (
    <label className="settings-input-label">
      {label}
      <select className="settings-select" value={value} onChange={(event) => onChange(event.target.value)} aria-label={label}>
        {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    </label>
  );
}

function SwitchButton({ checked, label, onChange }: { checked: boolean; label: string; onChange: (checked: boolean) => void }) {
  return (
    <Button
      type="button"
      className={`switch-button ${checked ? "checked" : ""}`}
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
    >
      <span>{checked ? "On" : "Off"}</span>
    </Button>
  );
}
