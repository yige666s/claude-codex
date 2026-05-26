package agentruntime

import backendretry "claude-codex/internal/backend/retry"

type RetryPolicy = backendretry.Policy
type RetryAfterProvider = backendretry.RetryAfterProvider
type RetryAfterHeaderProvider = backendretry.RetryAfterHeaderProvider
type HTTPResponseProvider = backendretry.HTTPResponseProvider

var ParseRetryAfter = backendretry.ParseRetryAfter
