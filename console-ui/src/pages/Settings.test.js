import { describe, expect, it } from "vitest";
import { diagnosticsTools, settingsUpdatePayload } from "./settingsPayload";

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
    expect(payload).not.toHaveProperty("forceTwoFactor");
  });
});

describe("network and diagnostics settings", () => {
  it("keeps every existing diagnostics entry alongside network settings", () => {
    expect(diagnosticsTools.map((tool) => tool.name)).toEqual([
      "网络连通性",
      "带宽测试",
      "系统日志",
      "一键体检",
      "一键诊断包",
      "远程重启",
    ]);
  });
});
