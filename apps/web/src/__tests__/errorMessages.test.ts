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
});
