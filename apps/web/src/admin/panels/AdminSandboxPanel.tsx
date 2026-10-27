import { useEffect, useState } from "react";
import { ApiClient } from "../../api/client";
import { Button } from "../../components/ui/button";

const emptyPolicy = { enabled: false, default_profile: "", profiles: {}, routes: [], node_pools: {} };

type Revision = { revision: number; document: Record<string, unknown> };

export function AdminSandboxPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [active, setActive] = useState<Revision | null>(null);
  const [editor, setEditor] = useState(JSON.stringify(emptyPolicy, null, 2));
  const [draftID, setDraftID] = useState<number | null>(null);
  const [rollbackID, setRollbackID] = useState("");
  const [revisions, setRevisions] = useState<Array<{ revision: number; state: string; actor: string }>>([]);
  const [origin, setOrigin] = useState("unknown");
  const [preview, setPreview] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    const [response, history] = await Promise.all([api.adminSandboxPolicy(adminToken), api.adminSandboxPolicyRevisions(adminToken)]);
    setRevisions(history.revisions || []);
    setActive(response.active);
    setEditor(JSON.stringify(response.active.document, null, 2));
    setDraftID(null);
  }

  useEffect(() => { if (adminToken) load().catch((error) => setMessage(String(error))); }, [adminToken]);

  async function action(kind: "validate" | "drafts" | "publish" | "rollback" | "preview") {
    setBusy(true);
    setMessage("");
    try {
      if (kind === "validate") {
        await api.adminSandboxPolicyAction(adminToken, kind, JSON.parse(editor));
        setMessage("Policy is valid. This does not enable execution.");
      } else if (kind === "drafts") {
        const response = await api.adminSandboxPolicyAction<{ draft: Revision }>(adminToken, kind, JSON.parse(editor));
        setDraftID(response.draft.revision);
        setMessage(`Draft ${response.draft.revision} saved. Publish separately.`);
      } else if (kind === "preview") {
        const response = await api.adminSandboxPolicyAction<{ profile: string; routed: boolean; simulation_only: boolean }>(adminToken, kind, { origin });
        setPreview(response.routed ? `Profile: ${response.profile} (simulation only)` : "No route: execution disabled");
      } else {
        const revision = kind === "publish" ? draftID : Number(rollbackID);
        if (!revision || !active) throw new Error("A draft/source revision and active revision are required");
        await api.adminSandboxPolicyAction(adminToken, kind, { revision, expected_active: active.revision });
        await load();
        setMessage(`${kind} completed. Node job admission is still disabled.`);
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }

  return <section className="admin-sandbox-panel" style={{ display: "grid", gap: 16, maxWidth: 980 }}>
    <p>Active revision: {active?.revision ?? "unavailable"}. Policy changes cannot enable native execution or submit jobs.</p>
    <label htmlFor="sandbox-policy-json">Sandbox profiles, routes, and node pools (non-secret JSON)</label>
    <textarea id="sandbox-policy-json" value={editor} onChange={(event) => setEditor(event.target.value)} spellCheck={false} rows={20} style={{ width: "100%", fontFamily: "monospace" }} />
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
      <Button onClick={() => action("validate")} disabled={busy}>Validate</Button>
      <Button onClick={() => action("drafts")} disabled={busy}>Save draft</Button>
      <Button onClick={() => action("publish")} disabled={busy || !draftID}>Publish draft {draftID ?? ""}</Button>
      <Button onClick={() => load().catch((error) => setMessage(String(error)))} disabled={busy}>Reload</Button>
    </div>
    <div style={{ display: "flex", gap: 8 }}>
      <label htmlFor="sandbox-policy-origin">Preview origin</label>
      <select id="sandbox-policy-origin" value={origin} onChange={(event) => setOrigin(event.target.value)}>
        <option value="unknown">unknown</option><option value="external">external</option><option value="generated">generated</option><option value="repo_release">repo_release</option>
      </select>
      <Button onClick={() => action("preview")} disabled={busy}>Preview route</Button><span>{preview}</span>
    </div>
    <div>Recent revisions: {revisions.map((item) => `${item.revision} (${item.state})`).join(", ") || "none"}</div>
    <div style={{ display: "flex", gap: 8 }}>
      <label htmlFor="sandbox-policy-rollback">Rollback from revision</label>
      <input id="sandbox-policy-rollback" type="number" min="1" value={rollbackID} onChange={(event) => setRollbackID(event.target.value)} />
      <Button onClick={() => action("rollback")} disabled={busy || !rollbackID}>Rollback</Button>
    </div>
    {message && <p role="status">{message}</p>}
  </section>;
}
