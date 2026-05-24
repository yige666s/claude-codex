export function userFacingErrorMessage(message: string): string {
  const text = (message || "").trim();
  if (!text) return "Request failed.";

  if (/origin is not allowed/i.test(text)) {
    return "This browser origin is not allowed for this AgentAPI deployment.";
  }
  if (/csrf/i.test(text)) {
    return "Your session security check failed. Refresh the page and try again.";
  }
  if (/failed to fetch|networkerror|load failed/i.test(text)) {
    return "Cannot reach AgentAPI. Check the network connection and try again.";
  }
  if (isCredentialConfigurationError(text)) {
    return "A service credential is missing or unavailable. Ask an administrator to verify the provider setup.";
  }
  if (isProviderAccessError(text)) {
    return "The AI provider rejected the request. Ask an administrator to verify provider access.";
  }
  if (isSandboxError(text)) {
    return "The tool sandbox could not start. Ask an administrator to check the sandbox configuration.";
  }
  if (looksLikeInternalPathLeak(text) || looksLikeStackTrace(text)) {
    return "The request failed because of an internal service configuration issue.";
  }
  return text;
}

function isCredentialConfigurationError(text: string): boolean {
  return /GOOGLE_APPLICATION_CREDENTIALS|GOOGLE_APPLICATION_CREDENTIALS_JSON|VERTEX_ACCESS_TOKEN|VERTEX_SERVICE_ACCOUNT|service account|vertex-service-account|api[_ -]?key|access token is required|credential/i.test(text);
}

function isProviderAccessError(text: string): boolean {
  return /(vertex|gemini|openai|anthropic|qwen|dashscope).*(unauthorized|forbidden|permission denied|quota|billing|invalid api key|401|403)/i.test(text);
}

function isSandboxError(text: string): boolean {
  return /sandbox|OCI runtime|docker:|container.*permission denied|operation not permitted|seccomp|apparmor/i.test(text);
}

function looksLikeInternalPathLeak(text: string): boolean {
  return /(?:^|\s)(?:\/run\/agentapi|\/opt\/agentapi|\/var\/lib\/agentapi|\/workspace|\/tmp|secrets\/)[^\s]*/i.test(text);
}

function looksLikeStackTrace(text: string): boolean {
  return /\n\s*at\s+\S+\s*\(|goroutine \d+ \[|\.go:\d+|\.ts:\d+|\.tsx:\d+/i.test(text);
}
