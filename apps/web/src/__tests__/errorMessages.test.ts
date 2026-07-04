import { describe, expect, it } from "vitest";
import { userFacingErrorMessage } from "../api/errorMessages";

describe("userFacingErrorMessage", () => {
  it("hides credential paths and provider env names", () => {
    expect(userFacingErrorMessage("live vertex access token is required: read GOOGLE_APPLICATION_CREDENTIALS: open /run/agentapi/secrets/vertex-service-account.json: no such file or directory"))
      .toBe("A service credential is missing or unavailable. Ask an administrator to verify the provider setup.");
  });

  it("hides sandbox internals", () => {
    expect(userFacingErrorMessage("docker: Error response from daemon: OCI runtime create failed: operation not permitted"))
      .toBe("The tool sandbox could not start. Ask an administrator to check the sandbox configuration.");
  });

  it("keeps ordinary validation errors", () => {
    expect(userFacingErrorMessage("password must be at least 8 characters")).toBe("password must be at least 8 characters");
  });

  it("explains deleted chat sessions without exposing internal IDs", () => {
    expect(userFacingErrorMessage("session not found (c564c98a2302/yhpw4XPnBE-000686)"))
      .toBe("关联的聊天会话已删除。");
  });

  it("hides low-level missing row errors", () => {
    expect(userFacingErrorMessage("sql: no rows in result set")).toBe("关联记录不存在或已被删除。");
  });
});
