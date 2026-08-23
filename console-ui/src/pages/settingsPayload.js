import { T } from "../tokens";

export const diagnosticsTools = [
  { id: "ping", icon: "network", name: "网络连通性", desc: "ping 云端 · 网关 · DNS", color: T.blue },
  { id: "bandwidth", icon: "arrowUp", name: "带宽测试", desc: "上行 / 下行 · 抖动 · 丢包", color: T.indigo },
  { id: "syslog", icon: "terminal", name: "系统日志", desc: "journalctl · dmesg · syslog", color: T.violet },
  { id: "health", icon: "shield", name: "一键体检", desc: "硬件 · 驱动 · 模型完整性", color: T.green },
  { id: "bundle", icon: "download", name: "一键诊断包", desc: "日志 + 配置 + 指标 · 离线上报", color: T.amber },
  { id: "reboot", icon: "power", name: "远程重启", desc: "需管理员权限", color: T.red },
];

export function settingsUpdatePayload(settings, overrides = {}) {
  return {
    httpPort: settings.httpPort,
    httpsPort: settings.httpsPort,
    shareDomain: settings.shareDomain || "",
    maxUploadBytesSec: settings.maxUploadBytesSec || 0,
    maxDownloadBytesSec: settings.maxDownloadBytesSec || 0,
    accessCodeEnabled: !!settings.accessCodeEnabled,
    httpsCertificate: settings.httpsCertificate || "",
    ddnsProvider: settings.ddnsProvider || "",
    ddnsDomain: settings.ddnsDomain || "",
    ddnsCredentialRef: settings.ddnsCredentialRef || "",
    ddnsWebhookURL: settings.ddnsWebhookURL || "",
    ...overrides,
  };
}
