# Frontend shadcn Refactor Implementation Plan

## Context

The first shadcn migration pass added the component foundation without replacing
the whole UI at once. Current state:

- shadcn-compatible config exists at `apps/web/components.json`.
- Shared UI components live under `apps/web/src/components/ui/`.
- `cn` helper lives at `apps/web/src/lib/utils.ts`.
- Vite/Tsconfig now support `@/*` aliases.
- Tailwind v4 Vite plugin is installed, while the app still mostly relies on
  `apps/web/src/styles/app.css`.
- Initial usage is in login/register, global search, confirmation dialogs,
  right-panel tabs, and parts of Admin navigation.

The next phase should continue incremental replacement. Avoid a single large
rewrite of `App.tsx` or `AdminConsole.tsx`; both files are large and carry many
behavioral edge cases.

## Goals

1. Standardize core controls on shadcn-style components.
2. Reduce one-off CSS for buttons, badges, inputs, tabs, dialogs, and tables.
3. Keep the existing application behavior unchanged.
4. Improve visual consistency in the chat workspace and Admin/Evaluation areas.
5. Keep bundle size and lazy-loading behavior at least as good as the current
   implementation.

## Non-Goals

- Do not redesign the product flow or information architecture.
- Do not replace all CSS with Tailwind utilities in one pass.
- Do not merge Admin and chat UI concerns into a single giant abstraction.
- Do not introduce a new state-management library.
- Do not change API contracts while doing visual refactor work.

## Proposed Workstreams

### 1. Complete UI Primitive Layer

Add shadcn-style components needed by the existing screens:

- `select`
- `switch`
- `checkbox`
- `label`
- `separator`
- `scroll-area`
- `table`
- `alert`
- `tooltip`
- `dropdown-menu`
- `sheet`
- `progress`

Use Radix primitives where useful. Keep exported APIs close to shadcn defaults
so future `shadcn` component additions are predictable.

Acceptance criteria:

- Components are in `apps/web/src/components/ui/`.
- Components use `cn` and shared CSS classes or Tailwind-compatible classes.
- `npm run build` passes.
- No unused component exports are wired into pages until actually needed.

### 2. AdminConsole Forms and Filters

Refactor repeated Admin filter controls first. These are high-duplication and
low-risk:

- Admin token input
- skill/user/job/audit/evaluation filter rows
- status/time-window selects
- evaluation create-run form
- LLM governance config inputs

Recommended extraction:

- `AdminFilterBar`
- `AdminField`
- `AdminActionRow`
- `AdminSectionNotice`

Acceptance criteria:

- Existing admin queries still work.
- Required inputs keep labels and `aria-label`s.
- Dense operational layout remains compact; do not turn Admin into a marketing
  card layout.
- `npm run build` and `npm run test` pass.

### 3. Admin Tables and Lists

Replace ad hoc `admin-table`, `admin-table-row`, and button-row patterns with a
shared table/list surface:

- skill execution rows
- user sessions
- job rows
- event timelines
- audit records
- evaluation results/reviews
- LLM usage records

Recommended components:

- `DataTable`
- `DataList`
- `StatusBadge`
- `MetricCard`

Acceptance criteria:

- Row click behavior remains unchanged.
- Status colors remain readable and consistent.
- Long IDs, model names, and error messages truncate gracefully.
- Mobile widths do not overflow horizontally except where table scrolling is
  intentional.

### 4. Dialog and Modal Migration

Move remaining custom modal surfaces to `Dialog`:

- `SkillDetailModal`
- `SettingsModal`
- `MemoryModal`
- `PreviewModal`
- `SkillPolicyModal`

Keep special sizing where needed. For large surfaces, use a shadcn-style
`Sheet` or a large `DialogContent` variant instead of nested cards.

Acceptance criteria:

- Escape closes the modal.
- Focus is trapped.
- Close buttons are reachable by keyboard.
- Preview modal still supports images, PDF, text, docx preview iframe, and
  download.

### 5. Chat Workspace Controls

Refactor the frequently used chat controls:

- sidebar toolbar buttons
- session row delete button
- settings dropdown
- composer upload/send/stop/live buttons
- input mode segmented control
- right-panel search input
- job cancel button
- attachment/artifact action buttons

Acceptance criteria:

- Text mode and live mode controls remain visually distinct.
- Composer height auto-resize still works.
- Upload progress, pending attachments, and errors still render correctly.
- No layout shift when toggling live mode or response timing indicators.

### 6. CSS Cleanup

After components are migrated, remove dead CSS from `app.css` in focused passes.

Suggested order:

1. button/icon/button variants
2. inputs/selects/textareas
3. tabs/badges/pills/statuses
4. modal/dialog styles
5. admin table/list styles

Rules:

- Run `rg` before deleting selectors.
- Delete only selectors with no remaining usage.
- Keep design tokens in `:root`.
- Keep responsive layout CSS until the relevant layout is explicitly refactored.

## Verification Plan

Run after every meaningful migration slice:

```bash
cd apps/web
npm run build
npm run test
```

For visual verification, run the dev server and inspect at least:

- logged-out login page
- main chat workspace
- global search modal
- right panel tabs: Skills, Jobs, Attachments, Artifacts
- Admin Skills page
- Admin Evaluation page
- Settings modal
- Preview modal

Recommended Playwright screenshots:

- desktop: `1440x900`
- tablet: `1024x768`
- mobile: `390x844`

Visual checks:

- no overlapping text
- no clipped button labels
- tab counts remain aligned
- modals are centered and scroll correctly
- admin filters remain usable in narrow side panels

## Deployment Notes

- Frontend changes are deployed through the GHCR workflow.
- Do not run application image builds on the test server.
- After merging to `main`, verify GitHub Actions builds and deploys:

```bash
gh run list --repo yige666s/claude-codex --workflow deploy-main.yml --limit 3
```

Then verify the public service:

```bash
curl -fsS https://www.mkason.com/readyz
```

## Risk Register

- **Radix Dialog + existing focus trap conflicts**: remove old `useFocusTrap`
  usage per modal as it migrates to Radix Dialog.
- **CSS selector collisions**: new UI classes use `ui-*` prefixes to avoid
  clobbering existing global button/input rules.
- **Admin layout bloat**: keep Admin components dense and operational.
- **Bundle growth**: Admin remains lazy-loaded; avoid importing Admin-only UI
  into `App.tsx` if it is not needed by chat users.
- **Tailwind partial adoption**: Tailwind is available but existing CSS remains
  authoritative until cleanup passes remove old selectors.

## Suggested Commit Slices

1. Add missing primitives only.
2. Migrate Admin filters and notices.
3. Migrate Admin tables/lists.
4. Migrate remaining modals.
5. Migrate chat composer and right-panel controls.
6. Delete dead CSS.

Each slice should include:

- changed files summary
- build/test evidence
- screenshots for visual slices
- remaining risks
