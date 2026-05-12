# AgentAPI Web App

Consumer-facing React/Vite frontend for `cmd/agentapi`, including the main
workspace UI and the `/admin` operations console.

The site logo is served from `public/logo.png`. The same file is used for the
browser icon, login screen brand mark, sidebar brand mark, and admin console
brand mark.

## Development

Start the backend on `http://localhost:8081`, then run:

```bash
cd apps/web
npm install
npm run dev
```

The Vite dev server proxies `/v1`, `/healthz`, `/readyz`, and `/metrics` to
`http://localhost:8081`. Override with:

```bash
AGENT_API_DEV_TARGET=http://localhost:8082 npm run dev
```

The backend must allow the Vite origin during local development:

```bash
-cors-allowed-origins http://localhost:8081,http://localhost:5173
```

## Production API Configuration

By default the production bundle calls AgentAPI through relative paths such as
`/v1` and `/readyz`. This is the preferred setup when a reverse proxy serves the
SPA and AgentAPI from the same origin.

For split-origin hosting, build with an absolute API origin:

```bash
VITE_AGENT_API_BASE_URL=https://api.example.com npm run build
```

Copy `.env.production.example` when wiring platform build settings. The same
base URL is used for JSON APIs, attachment/artifact downloads, and job SSE
streams.

See `deploy/production/FRONTEND.md` for static hosting, CORS, cookie, and
same-origin reverse-proxy guidance.

## Scripts

- `npm run dev`: local development server.
- `npm run build`: TypeScript check and production build.
- `npm run test`: Vitest unit tests.
- `npm run e2e`: Playwright browser E2E tests.
- `npm run e2e:ui`: Playwright interactive test runner.
- `npm run preview`: preview the production build.

## Product Surface

The current frontend includes:

- Login/register/logout with stored access and refresh tokens.
- Registration email-verification feedback.
- Automatic access-token refresh before API calls.
- Session list, auto-created initial session, chat transcript, and SSE chat
  event handling.
- Long-running job list, timeline replay, EventSource live updates, and
- cancellation. The newest job opens by default and older jobs stay collapsed.
- Attachment upload/list/download/delete with progress and preview.
- Artifact list/preview/download/delete.
- Skill category/search/detail UI with prompt insertion.
- Memory settings, memory management, export/delete data actions, and account
  deletion flows.
- Global search across sessions.
- Admin console for skill management, users, sessions/jobs/artifacts,
  health/cost, audit logs, and risk operations.
- Responsive layout for desktop and mobile.

The embedded `agentruntime` UI remains useful as a backend debug console. This
app is the product-facing frontend and should evolve independently.
