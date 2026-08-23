export function settingsUpdatePayload(settings, overrides = {}) {
  return {
    httpPort: settings.httpPort,
    httpsPort: settings.httpsPort,
    shareDomain: settings.shareDomain || "",
    maxUploadBytesSec: settings.maxUploadBytesSec || 0,
    maxDownloadBytesSec: settings.maxDownloadBytesSec || 0,
    accessCodeEnabled: !!settings.accessCodeEnabled,
    forceTwoFactor: !!settings.forceTwoFactor,
    httpsCertificate: settings.httpsCertificate || "",
    ddnsProvider: settings.ddnsProvider || "",
    ddnsDomain: settings.ddnsDomain || "",
    ddnsCredentialRef: settings.ddnsCredentialRef || "",
    ddnsWebhookURL: settings.ddnsWebhookURL || "",
    ...overrides,
  };
}
