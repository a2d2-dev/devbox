import { describe, expect, it } from "vitest";
import { settingsUpdatePayload } from "./settingsPayload";

describe("settingsUpdatePayload", () => {
  it("only sends editable fields and applies sensitive overrides explicitly", () => {
    const payload = settingsUpdatePayload(
      {
        httpPort: 9133,
        httpsPort: 9443,
        shareDomain: "https://share.example.com",
        accessCodeConfigured: true,
        totpEnabled: true,
        ddnsCredentialRef: "env:CLOUDFLARE_TOKEN",
      },
      { accessCode: "new-code" },
    );

    expect(payload).toMatchObject({
      httpPort: 9133,
      httpsPort: 9443,
      shareDomain: "https://share.example.com",
      ddnsCredentialRef: "env:CLOUDFLARE_TOKEN",
      accessCode: "new-code",
    });
    expect(payload).not.toHaveProperty("accessCodeConfigured");
    expect(payload).not.toHaveProperty("totpEnabled");
  });
});
