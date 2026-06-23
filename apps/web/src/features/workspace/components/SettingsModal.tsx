import { useEffect, useState } from "react";
import { Brain, ChevronDown, ChevronUp, Database, Plug, UserX, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";
import { Textarea } from "../../../components/ui/textarea";
import type { ConnectorPolicy, ConnectorStatus, MemorySettings, PersonalizationSettings } from "../../../types";

type SettingsSection = "personalization" | "connectors" | "data" | "account";

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
  initialSection = "personalization",
  connectors,
  connectorBusy,
  connectorNotice,
  hasSession,
  onUpdateMemorySettings,
  onUpdatePersonalization,
  onResetPersonalization,
  onConnectProvider,
  onUpdateConnectorPolicy,
  onDisconnectProvider,
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
  initialSection?: SettingsSection;
  connectors: ConnectorStatus[];
  connectorBusy: string;
  connectorNotice?: string;
  hasSession: boolean;
  onUpdateMemorySettings: (patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>) => void;
  onUpdatePersonalization: (patch: Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">>) => void;
  onResetPersonalization: () => void;
  onConnectProvider: (provider: string) => void;
  onUpdateConnectorPolicy: (provider: string, policy: ConnectorPolicy) => void;
  onDisconnectProvider: (provider: string) => void;
  onManageMemory: () => void;
  onDeleteSessionMemory: () => void;
  onDeleteAllMemory: () => void;
  onExportData: () => void;
  onDeleteAccount: () => void;
  onLogout: () => void;
  onClose: () => void;
}) {
  const [activeSection, setActiveSection] = useState<SettingsSection>(initialSection);
  const [draftPersonalization, setDraftPersonalization] = useState<PersonalizationSettings>(personalizationSettings);

  useEffect(() => {
    setDraftPersonalization(personalizationSettings);
  }, [personalizationSettings]);

  useEffect(() => {
    setActiveSection(initialSection);
  }, [initialSection]);

  const personalizationDirty = JSON.stringify(normalizePersonalizationDraft(draftPersonalization)) !== JSON.stringify(normalizePersonalizationDraft(personalizationSettings));

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onClose();
    }}>
      <DialogContent className="settings-modal" hideClose>
        <DialogTitle className="sr-only">Settings</DialogTitle>
        <DialogDescription className="sr-only">Manage personalization, data controls, and account actions.</DialogDescription>
        <aside className="settings-nav" aria-label="Settings sections">
          <Button className="icon settings-close" onClick={onClose} title="Close settings" aria-label="Close settings">
            <X size={22} />
          </Button>
          <Button className={`settings-nav-item ${activeSection === "personalization" ? "active" : ""}`} onClick={() => setActiveSection("personalization")}><Brain size={18} /> Personalization</Button>
          <Button className={`settings-nav-item ${activeSection === "connectors" ? "active" : ""}`} onClick={() => setActiveSection("connectors")}><Plug size={18} /> Connectors</Button>
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
          {activeSection === "connectors" && (
            <ConnectorsSettingsPanel
              connectors={connectors}
              busyProvider={connectorBusy}
              notice={connectorNotice}
              onConnect={onConnectProvider}
              onPolicyChange={onUpdateConnectorPolicy}
              onDisconnect={onDisconnectProvider}
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

const connectorPolicyOptions: Array<{ value: ConnectorPolicy; label: string }> = [
  { value: "read_only", label: "Read only" },
  { value: "draft_write", label: "Draft writes" },
  { value: "write_with_review", label: "Review writes" },
  { value: "disabled", label: "Disabled" }
];
const connectorToolPreviewLimit = 6;

function ConnectorsSettingsPanel({
  connectors,
  busyProvider,
  notice,
  onConnect,
  onPolicyChange,
  onDisconnect
}: {
  connectors: ConnectorStatus[];
  busyProvider: string;
  notice?: string;
  onConnect: (provider: string) => void;
  onPolicyChange: (provider: string, policy: ConnectorPolicy) => void;
  onDisconnect: (provider: string) => void;
}) {
  return (
    <>
      <header>
        <div>
          <h2>Connectors</h2>
          <small>{connectors.filter((item) => item.connection?.status === "connected").length} connected</small>
        </div>
      </header>
      {notice && (
        <div className="connector-success-banner" role="status">
          <strong>{notice}</strong>
        </div>
      )}
      <div className="connector-list">
        {connectors.map((item) => {
          const connection = item.connection;
          const connected = connection?.status === "connected";
          const busy = busyProvider === item.provider.id;
          return (
            <section className="connector-card" key={item.provider.id}>
              <div className="connector-card-main">
                <div>
                  <div className="connector-title-row">
                    <strong>{item.provider.name}</strong>
                    <span className={`connector-status ${connected ? "connected" : connection?.status === "disabled" ? "disabled" : ""}`}>
                      {connected ? "Connected" : connection?.status === "disabled" ? "Disabled" : "Not connected"}
                    </span>
                    {!item.provider.configured && <span className="connector-status warning">OAuth not configured</span>}
                  </div>
                  <p>{item.provider.description}</p>
                </div>
                <div className="settings-action-group">
                  {connected ? (
                    <button className="settings-action danger-outline" type="button" disabled={busy} onClick={() => onDisconnect(item.provider.id)}>
                      Disconnect
                    </button>
                  ) : (
                    <button className="settings-action primary" type="button" disabled={busy} onClick={() => onConnect(item.provider.id)}>
                      {busy ? "Connecting..." : "Connect"}
                    </button>
                  )}
                </div>
              </div>
              <div className="connector-meta-grid">
                <ConnectorFact label="Scope" value={connection?.scopes?.join(", ") || item.provider.scopes.join(", ")} />
                <ConnectorFact label="Last sync" value={formatConnectorTime(connection?.last_sync_at)} />
                <ConnectorFact label="Context" value={item.context.task_types.join(", ")} />
                <ConnectorFact label="Runtime" value={formatConnectorKind(item.provider.connection_kind, item.provider.supports_synced_index)} />
                <ConnectorFact label="MCP server" value={formatMCPServer(item.mcp_server)} />
                <ConnectorFact label="Discovery" value={formatConnectorTime(item.mcp_server?.last_discovered_at)} />
                <label className="connector-policy-field">
                  <span>Policy</span>
                  <select
                    className="settings-select"
                    value={connection?.permission_policy || item.provider.default_policy}
                    disabled={!connection || busy}
                    onChange={(event) => onPolicyChange(item.provider.id, event.target.value as ConnectorPolicy)}
                  >
                    {connectorPolicyOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                  </select>
                </label>
              </div>
              <ConnectorMCPError server={item.mcp_server} />
              <ConnectorTools tools={item.mcp_tools || []} />
              <small className="connector-policy-note">{item.context.policy_hint}</small>
            </section>
          );
        })}
      </div>
    </>
  );
}

function ConnectorMCPError({ server }: { server?: ConnectorStatus["mcp_server"] }) {
  const error = formatMCPDiscoveryError(server);
  if (!error) return null;
  const match = error.match(/https?:\/\/\S+/);
  const url = match?.[0]?.replace(/[")\]}.,]+$/, "");
  return (
    <div className="connector-error-note" role="status">
      <strong>MCP discovery error</strong>
      <span>{error}</span>
      {url && (
        <a href={url} target="_blank" rel="noreferrer">
          Open provider setup
        </a>
      )}
    </div>
  );
}

function ConnectorTools({ tools }: { tools: ConnectorStatus["mcp_tools"] }) {
  const [expanded, setExpanded] = useState(false);
  if (!tools?.length) return null;
  const visibleTools = expanded ? tools : tools.slice(0, connectorToolPreviewLimit);
  const hiddenCount = Math.max(0, tools.length - visibleTools.length);
  const readCount = tools.filter((tool) => tool.side_effect_level === "read").length;
  const reviewCount = tools.filter((tool) => tool.requires_review).length;
  const disabledCount = tools.filter((tool) => !tool.allowed).length;
  const summary = [
    `${tools.length} tools`,
    `${readCount} read`,
    reviewCount ? `${reviewCount} review` : "",
    disabledCount ? `${disabledCount} disabled` : ""
  ].filter(Boolean).join(" · ");
  return (
    <div className={`connector-tools ${expanded ? "expanded" : ""}`}>
      <div className="connector-tools-header">
        <div>
          <span>MCP tools</span>
          <strong>{summary}</strong>
        </div>
        {tools.length > connectorToolPreviewLimit && (
          <button
            className="connector-tools-toggle"
            type="button"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            {expanded ? "Show less" : `Show all ${tools.length}`}
          </button>
        )}
      </div>
      <div className="connector-tool-list" aria-label="MCP tools">
        {visibleTools.map((tool) => (
          <span className={`connector-tool-chip ${tool.allowed ? "" : "disabled"}`} key={tool.policy_id || tool.tool_name}>
            <strong>{tool.tool_name}</strong>
            <small>{formatToolPolicy(tool.permission_policy, tool.requires_review, tool.side_effect_level)}</small>
          </span>
        ))}
        {!expanded && hiddenCount > 0 && (
          <span className="connector-tool-chip more">
            <strong>+{hiddenCount} more</strong>
          </span>
        )}
      </div>
    </div>
  );
}

function ConnectorFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="connector-fact">
      <span>{label}</span>
      <strong>{value || "None"}</strong>
    </div>
  );
}

function formatConnectorTime(value?: string) {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function formatMCPServer(server?: ConnectorStatus["mcp_server"]) {
  if (!server) return "None";
  const transport = server.transport || "mcp";
  return `${transport} · ${server.status}`;
}

function formatMCPDiscoveryError(server?: ConnectorStatus["mcp_server"]) {
  const raw = typeof server?.metadata?.last_discovery_error === "string" ? server.metadata.last_discovery_error : "";
  if (!raw || server?.status !== "error") return "";
  return raw
    .replace(/\\\//g, "/")
    .replace(/^mcp\s+\w+\s+tools\/list\s+failed\s+\(\d+\):\s*/i, "")
    .replace(/^mcp\s+\w+\s+initialize\s+failed\s+\(\d+\):\s*/i, "")
    .trim();
}

function formatToolPolicy(policy: ConnectorPolicy, review: boolean, sideEffect: string) {
  if (review) return `${policy} · review`;
  return `${policy} · ${sideEffect || "unknown"}`;
}

function formatConnectorKind(kind?: string, synced?: boolean) {
  if (synced) return "Synced index + MCP";
  if (kind === "mcp_builtin_adapter") return "Built-in MCP adapter";
  if (kind === "mcp_remote") return "Remote MCP";
  if (kind === "synced_index") return "Synced index";
  return "Connector";
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
