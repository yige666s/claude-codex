# Frontend Phase 0 Baseline

This directory contains Playwright coverage for the visual baseline used before
the interaction modernization work.

Run the baseline capture from `apps/web`:

```bash
npx playwright test e2e/frontend-baseline.spec.ts
```

Screenshots are written to:

```text
apps/web/test-results/frontend-baseline/
```

The baseline spec mocks the AgentAPI HTTP surface so it can run without a live
backend. It captures:

- desktop workspace
- desktop settings menu
- desktop right-panel jobs
- desktop admin shell
- mobile workspace
- mobile navigation drawer

These screenshots are intentionally generated artifacts and should not be
committed unless a reviewer explicitly asks for golden image fixtures.
