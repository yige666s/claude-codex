# AgentAPI Frontend Deployment

The product frontend in `apps/web` is a static Vite build. Production should
serve it from a static host or CDN, with AgentAPI exposed either on the same
origin through a reverse proxy or on a separate API origin.

## Recommended Option: Same-Origin Reverse Proxy

Use one public origin, for example `https://app.example.com`:

- Serve `apps/web/dist` as static files.
- Reverse-proxy `/v1/*`, `/healthz`, `/readyz`, and `/metrics` to `agentapi`.
- Build the frontend with `VITE_AGENT_API_BASE_URL` unset so browser API calls
  stay relative to the current origin.
- Keep `AGENT_API_CORS_ALLOWED_ORIGINS` empty or set it to the same public
  origin; same-origin browser requests do not require CORS.

Example build:

```bash
cd apps/web
npm ci
npm run build
```

Example container build:

```bash
docker build -f Dockerfile.agentweb \
  --build-arg VITE_AGENT_API_BASE_URL= \
  -t claude-codex-agentweb:prod .
```

`deploy/production/frontend-nginx.conf` serves the SPA and includes commented
reverse-proxy blocks for the API paths.

## Separate Frontend And API Origins

Use this when the frontend is hosted on a CDN/static host and the API is on a
separate origin, for example:

- Frontend: `https://app.example.com`
- API: `https://api.example.com`

Build the frontend with an absolute API base URL:

```bash
cd apps/web
VITE_AGENT_API_BASE_URL=https://api.example.com npm run build
```

Configure AgentAPI to allow the frontend origin:

```bash
AGENT_API_CORS_ALLOWED_ORIGINS=https://app.example.com
AGENT_API_CORS_ALLOW_CREDENTIALS=true
```

If using cookie auth across subdomains, also configure:

```bash
AGENT_API_SESSION_COOKIE_DOMAIN=.example.com
AGENT_API_SESSION_COOKIE_SECURE=true
AGENT_API_SESSION_COOKIE_SAMESITE=none
AGENT_API_CSRF_ENABLED=true
```

Bearer-token JWT mode works with either same-origin or split-origin deployment.
Cookie mode needs HTTPS, credentialed CORS, SameSite=None for cross-site
subdomains, and CSRF enabled for unsafe methods.

## API Paths Used By The Frontend

The frontend calls:

- `/v1/*` for auth, sessions, chat, jobs, attachments, artifacts, search, and
  account/data actions.
- `/readyz` for service status.
- `/healthz` for basic availability checks.
- `/metrics` only when explicitly opened or proxied for operations.

When `VITE_AGENT_API_BASE_URL` is unset, these paths are relative. When set,
the value is prepended to every API, attachment/artifact download, and job SSE
URL.

## Cache Policy

Vite emits content-hashed files under `assets/`; these can be cached for a year
with `immutable`. Serve `index.html` with a short cache lifetime or
revalidation so new deployments can update the asset manifest quickly.
