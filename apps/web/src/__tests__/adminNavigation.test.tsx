import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import AdminConsole from "../admin/AdminConsole";
import {
  AdminContextSidebar,
  AdminRail,
  buildAdminDomains,
  domainForAdminSection,
  isAdminSection,
  type AdminSection
} from "../admin/AdminNavigation";

describe("admin navigation", () => {
  it("groups every admin section into exactly one management domain", () => {
    const domains = buildAdminDomains(12);
    const sections = domains.flatMap((domain) => domain.sections.map((section) => section.id));

    expect(domains.map((domain) => domain.id)).toEqual(["build", "operate", "observe", "govern"]);
    expect(sections).toEqual([
      "skills",
      "prompts",
      "jobs-assets",
      "health-cost",
      "audit",
      "users",
      "evaluation"
    ]);
    expect(new Set(sections).size).toBe(sections.length);
    expect(domains[0].sections[0].count).toBe(12);
  });

  it.each([
    ["skills", "build"],
    ["prompts", "build"],
    ["jobs-assets", "operate"],
    ["health-cost", "observe"],
    ["audit", "observe"],
    ["users", "govern"],
    ["evaluation", "govern"]
  ] as Array<[AdminSection, string]>)("maps %s to the %s domain", (section, domain) => {
    expect(domainForAdminSection(buildAdminDomains(0), section).id).toBe(domain);
  });

  it("accepts only routable admin section identifiers", () => {
    expect(isAdminSection("prompts")).toBe(true);
    expect(isAdminSection("jobs-assets")).toBe(true);
    expect(isAdminSection("settings")).toBe(false);
    expect(isAdminSection(null)).toBe(false);
  });

  it("renders the active domain and contextual section with accessible current-page state", () => {
    const domains = buildAdminDomains(3);
    const rail = renderToStaticMarkup(
      <AdminRail
        domains={domains}
        activeDomain="build"
        userLabel="Operator"
        onDomainChange={vi.fn()}
        onExit={vi.fn()}
        onAccess={vi.fn()}
        onCloseNavigation={vi.fn()}
      />
    );
    const sidebar = renderToStaticMarkup(
      <AdminContextSidebar
        domain={domains[0]}
        activeSection="prompts"
        userLabel="Operator"
        accessOpen={false}
        tokenConfigured
        adminTokenDraft=""
        onSectionChange={vi.fn()}
        onAccessToggle={vi.fn()}
        onAdminTokenDraftChange={vi.fn()}
        onSaveAccess={vi.fn()}
        onLogout={vi.fn()}
      />
    );

    expect(rail).toContain('aria-current="page"');
    expect(rail).toContain("Build");
    expect(sidebar).toContain('aria-label="Build administration"');
    expect(sidebar).toContain('aria-current="page"');
    expect(sidebar).toContain("Prompts");
    expect(sidebar).toContain("Production");
  });

  it("renders the Admin console as a domain rail plus contextual workspace", () => {
    const html = renderToStaticMarkup(
      <AdminConsole
        api={new ApiClient(() => undefined)}
        adminToken=""
        userLabel="Operator"
        onAdminTokenChange={vi.fn()}
        onExit={vi.fn()}
        onLogout={vi.fn()}
      />
    );

    expect(html).toContain('aria-label="Admin domains"');
    expect(html).toContain('aria-label="Build administration"');
    expect(html).toContain("Search or run a command...");
    expect(html).toContain("Admin token required");
    expect(html).not.toContain('class="admin-sidebar"');
  });
});
