import { useEffect, useState } from "react";
import { T } from "../tokens";
import { Icon } from "../icons";
import { authFetch } from "../hooks/useApi";
import { diagnosticsTools, settingsUpdatePayload } from "./settingsPayload";

const tabs = [
  ["network", "网口状态", "network"],
  ["remote", "远程访问", "globe"],
  ["ssh", "SSH", "terminal"],
  ["account", "账号安全", "lock"],
  ["bans", "异常封禁", "shield"],
  ["firewall", "防火墙", "shield"],
  ["certs", "证书", "key"],
  ["diagnostics", "诊断", "wrench"],
];

const panel = {
  background: T.surface,
  border: `1px solid ${T.border}`,
  borderRadius: 8,
  padding: 16,
  width: "100%",
  maxWidth: "100%",
  minWidth: 0,
};
const input = {
  height: 34,
  border: `1px solid ${T.border}`,
  borderRadius: 6,
  padding: "0 9px",
  fontSize: 12,
  color: T.ink,
  background: "#fff",
  minWidth: 0,
};
const button = {
  height: 32,
  border: `1px solid ${T.border}`,
  borderRadius: 6,
  padding: "0 11px",
  background: "#fff",
  color: T.ink2,
  fontSize: 12,
  fontWeight: 600,
  cursor: "pointer",
  display: "inline-flex",
  alignItems: "center",
  gap: 6,
};
const primary = {
  ...button,
  color: "#fff",
  background: T.blueDeep,
  borderColor: T.blueDeep,
};
const danger = {
  ...button,
  color: "#b91c1c",
  background: "#fff",
  borderColor: "#fecaca",
};

async function request(path, options = {}) {
  const r = await authFetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || data.message || `HTTP ${r.status}`);
  return data;
}

function Section({ title, note, actions, children }) {
  return (
    <section style={panel}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          marginBottom: 13,
        }}
      >
        <div>
          <div style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>
            {title}
          </div>
          {note && (
            <div style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>
              {note}
            </div>
          )}
        </div>
        <div style={{ flex: 1 }} />
        {actions}
      </div>
      {children}
    </section>
  );
}

function Badge({ children, tone = "gray" }) {
  const c = {
    green: ["#ecfdf5", "#047857"],
    red: ["#fef2f2", "#b91c1c"],
    blue: ["#e6f4ff", "#005eeb"],
    amber: ["#fffbeb", "#a16207"],
    gray: [T.surfaceAlt, T.ink3],
  }[tone];
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        padding: "2px 7px",
        borderRadius: 999,
        background: c[0],
        color: c[1],
        fontSize: 10.5,
        fontWeight: 650,
      }}
    >
      {children}
    </span>
  );
}
function Empty({ text }) {
  return (
    <div
      style={{
        padding: "20px 8px",
        textAlign: "center",
        color: T.ink3,
        fontSize: 12,
      }}
    >
      {text}
    </div>
  );
}
function Field({ label, children }) {
  return (
    <label
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 5,
        fontSize: 11,
        color: T.ink3,
        minWidth: 0,
      }}
    >
      {label}
      {children}
    </label>
  );
}
function ErrorBox({ error }) {
  return error ? (
    <div
      style={{
        padding: "9px 11px",
        background: "#fef2f2",
        border: "1px solid #fecaca",
        borderRadius: 6,
        color: "#991b1b",
        fontSize: 11.5,
      }}
    >
      {error}
    </div>
  ) : null;
}
function Preview({
  title = "待执行变更",
  text,
  onConfirm,
  confirmLabel = "确认 dry-run",
}) {
  return text ? (
    <div
      style={{
        marginTop: 12,
        border: "1px solid #99c7ff",
        borderRadius: 7,
        overflow: "hidden",
      }}
    >
      <div
        style={{
          padding: "8px 10px",
          background: "#e6f4ff",
          fontSize: 11.5,
          fontWeight: 700,
          color: "#0043b8",
          display: "flex",
          alignItems: "center",
        }}
      >
        {title}
        <div style={{ flex: 1 }} />
        {onConfirm && (
          <button style={primary} onClick={onConfirm}>
            {confirmLabel}
          </button>
        )}
      </div>
      <pre
        style={{
          margin: 0,
          padding: 12,
          maxHeight: 260,
          overflow: "auto",
          background: "#0f172a",
          color: "#cce4ff",
          fontSize: 11,
          lineHeight: 1.55,
          whiteSpace: "pre-wrap",
        }}
      >
        {typeof text === "string" ? text : JSON.stringify(text, null, 2)}
      </pre>
    </div>
  ) : null;
}
const fmtRate = (n) =>
  n
    ? `${(n / 1024).toFixed(n > 1024 * 1024 ? 0 : 1)} ${n > 1024 * 1024 ? "MB/s" : "KB/s"}`
    : "0 KB/s";

function NetworkTab({ network }) {
  const physical = (network?.interfaces || []).filter(
    (x) => !["virtual", "loopback"].includes(x.type),
  );
  const virtual = (network?.interfaces || []).filter((x) =>
    ["virtual", "loopback"].includes(x.type),
  );
  const table = (items) =>
    items.length ? (
      <div
        style={{
          display: "grid",
          gap: 8,
          overflowX: "auto",
          width: "100%",
          maxWidth: "100%",
          minWidth: 0,
        }}
      >
        {items.map((i) => (
          <div
            key={i.name}
            style={{
              border: `1px solid ${T.borderSoft}`,
              borderRadius: 7,
              padding: "11px 12px",
              display: "grid",
              gridTemplateColumns:
                "minmax(130px,1.1fr) minmax(170px,1.5fr) repeat(3,minmax(90px,1fr))",
              gap: 12,
              alignItems: "center",
              minWidth: 760,
            }}
          >
            <div>
              <div style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>
                {i.name}{" "}
                <Badge tone={i.state === "up" ? "green" : "gray"}>
                  {i.state}
                </Badge>
              </div>
              <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 4 }}>
                {i.type} · {i.mac || "无 MAC"}
              </div>
            </div>
            <div style={{ fontSize: 11.5, color: T.ink2, lineHeight: 1.7 }}>
              <div>IPv4: {(i.ipv4 || []).join(", ") || "-"}</div>
              <div>IPv6: {(i.ipv6 || []).join(", ") || "-"}</div>
              <div>
                网关: {i.gateway || "-"} · DNS:{" "}
                {(i.dns || []).join(", ") || "-"}
              </div>
            </div>
            <div>
              <div style={{ fontSize: 10.5, color: T.ink3 }}>配置</div>
              <div style={{ fontSize: 12, fontWeight: 650, marginTop: 4 }}>
                {i.mode?.toUpperCase()}
              </div>
            </div>
            <div>
              <div style={{ fontSize: 10.5, color: T.ink3 }}>链路</div>
              <div style={{ fontSize: 12, fontWeight: 650, marginTop: 4 }}>
                {i.linkMbps ? `${i.linkMbps} Mbps` : "-"} · {i.duplex || "-"}
              </div>
              <div style={{ fontSize: 10.5, color: T.ink3 }}>MTU {i.mtu}</div>
            </div>
            <div>
              <div style={{ fontSize: 10.5, color: T.ink3 }}>实时速率</div>
              <div
                className="mono"
                style={{ fontSize: 11.5, marginTop: 4, color: T.green }}
              >
                ↓ {fmtRate(i.rxBytesSec)}
              </div>
              <div className="mono" style={{ fontSize: 11.5, color: T.blue }}>
                ↑ {fmtRate(i.txBytesSec)}
              </div>
            </div>
          </div>
        ))}
      </div>
    ) : (
      <Empty text="未发现接口" />
    );
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="物理与隧道接口"
        note={`主地址 ${network?.ip || "-"} · 默认网关 ${network?.gateway || "-"}`}
      >
        {table(physical)}
      </Section>
      <Section title="虚拟与本地接口" note="分类展示，不参与默认管理入口选择">
        {table(virtual)}
      </Section>
    </div>
  );
}

function RemoteTab({ remote, settings, setSettings, reload }) {
  const [preview, setPreview] = useState("");
  const [settingsPreview, setSettingsPreview] = useState(null);
  const [result, setResult] = useState("");
  const [error, setError] = useState("");
  const ddns = {
    provider: settings.ddnsProvider || "",
    domain: settings.ddnsDomain || "",
    credentialRef: settings.ddnsCredentialRef || "",
    webhookURL: settings.ddnsWebhookURL || "",
  };
  const set = (k) => (e) => setSettings({ ...settings, [k]: e.target.value });
  const previewDDNS = async () => {
    try {
      setError("");
      const d = await request("/api/v1/network/ddns/preview", {
        method: "POST",
        body: JSON.stringify(ddns),
      });
      setPreview(d.preview);
    } catch (e) {
      setError(e.message);
    }
  };
  const save = async () => {
    try {
      setError("");
      const payload = settingsUpdatePayload(settings, {
        ddnsProvider: ddns.provider,
        ddnsDomain: ddns.domain,
        ddnsCredentialRef: ddns.credentialRef,
        ddnsWebhookURL: ddns.webhookURL,
      });
      const d = await request("/api/v1/security/settings/preview", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      setSettingsPreview({ payload, preview: d });
    } catch (e) {
      setError(e.message);
    }
  };
  const confirmSave = async () => {
    try {
      await request("/api/v1/security/settings", {
        method: "POST",
        body: JSON.stringify({ ...settingsPreview.payload, confirm: true }),
      });
      setSettingsPreview(null);
      setResult("配置已保存；端口或证书变更需重启 DevBox 生效");
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  const updateNow = async () => {
    try {
      const d = await request("/api/v1/network/ddns/update", {
        method: "POST",
        body: JSON.stringify({}),
      });
      setResult(d.result);
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="可用访问入口"
        note={`当前会话 ${remote?.currentSessionIP || "-"} · HTTPS ${remote?.https ? "已监听" : "未监听"}`}
      >
        <div
          style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}
        >
          <div>
            <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>
              tun0 隧道地址
            </div>
            {(remote?.tunnelIPs || []).map((x) => (
              <div
                key={x}
                className="mono"
                style={{ fontSize: 12, marginBottom: 4 }}
              >
                {x}
              </div>
            ))}
            {!remote?.tunnelIPs?.length && (
              <Badge tone="amber">隧道未启用</Badge>
            )}
          </div>
          <div>
            <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>
              监听地址
            </div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 5 }}>
              {(remote?.listeners || []).map((l, i) => (
                <Badge key={i} tone="blue">
                  {l.address}:{l.port}
                </Badge>
              ))}
            </div>
          </div>
        </div>
      </Section>
      <Section
        title="DDNS"
        note="凭据仅保存引用；立即更新在本机只执行 dry-run"
        actions={
          <>
            <button style={button} onClick={previewDDNS}>
              预览
            </button>
            <button style={primary} onClick={updateNow}>
              立即更新
            </button>
          </>
        }
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "150px 1fr 1fr",
            gap: 9,
          }}
        >
          <Field label="提供商">
            <select
              style={input}
              value={ddns.provider}
              onChange={set("ddnsProvider")}
            >
              <option value="">未配置</option>
              <option value="cloudflare">Cloudflare</option>
              <option value="webhook">自定义 Webhook</option>
            </select>
          </Field>
          <Field label="域名">
            <input
              style={input}
              value={ddns.domain}
              onChange={set("ddnsDomain")}
              placeholder="devbox.example.com"
            />
          </Field>
          <Field label="凭据引用">
            <input
              style={input}
              value={ddns.credentialRef}
              onChange={set("ddnsCredentialRef")}
              placeholder="env:CLOUDFLARE_TOKEN"
            />
          </Field>
        </div>
        {ddns.provider === "webhook" && (
          <Field label="Webhook URL">
            <input
              style={{ ...input, width: "100%", marginTop: 9 }}
              value={ddns.webhookURL}
              onChange={set("ddnsWebhookURL")}
              placeholder="https://..."
            />
          </Field>
        )}
        <Preview text={preview} />
      </Section>
      <Section
        title="外链与访问端口"
        note="分享域名供文件外链拼接；速率 0 表示不限速"
        actions={
          <button style={primary} onClick={save}>
            生成变更预览
          </button>
        }
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(130px,1fr))",
            gap: 9,
          }}
        >
          <Field label="分享域名">
            <input
              style={input}
              value={settings.shareDomain || ""}
              onChange={set("shareDomain")}
              placeholder="https://share.example.com"
            />
          </Field>
          <Field label="HTTP 端口">
            <input
              type="number"
              style={input}
              value={settings.httpPort || 9090}
              onChange={(e) =>
                setSettings({ ...settings, httpPort: +e.target.value })
              }
            />
          </Field>
          <Field label="HTTPS 端口">
            <input
              type="number"
              style={input}
              value={settings.httpsPort || 9443}
              onChange={(e) =>
                setSettings({ ...settings, httpsPort: +e.target.value })
              }
            />
          </Field>
          <Field label="上传 B/s">
            <input
              type="number"
              style={input}
              value={settings.maxUploadBytesSec || 0}
              onChange={(e) =>
                setSettings({ ...settings, maxUploadBytesSec: +e.target.value })
              }
            />
          </Field>
          <Field label="下载 B/s">
            <input
              type="number"
              style={input}
              value={settings.maxDownloadBytesSec || 0}
              onChange={(e) =>
                setSettings({
                  ...settings,
                  maxDownloadBytesSec: +e.target.value,
                })
              }
            />
          </Field>
        </div>
        <Preview
          text={settingsPreview?.preview}
          onConfirm={settingsPreview ? confirmSave : null}
          confirmLabel="确认保存"
        />
      </Section>
      <ErrorBox error={error} />
      {result && (
        <div style={{ fontSize: 11.5, color: "#047857" }}>{result}</div>
      )}
    </div>
  );
}

function SSHTab({ ssh }) {
  const [change, setChange] = useState({
    port: ssh?.port || 22,
    permitRootLogin: ssh?.permitRootLogin || "no",
    passwordAuthentication: ssh?.passwordAuthentication || "no",
  });
  const [preview, setPreview] = useState("");
  const [error, setError] = useState("");
  const run = async (apply = false) => {
    try {
      setError("");
      const path = apply
        ? "/api/v1/security/ssh/apply"
        : "/api/v1/security/ssh/preview";
      const body = apply ? { change, confirm: true, dryRun: true } : change;
      const d = await request(path, {
        method: "POST",
        body: JSON.stringify(body),
      });
      setPreview(d.diff + (d.message ? `\n\n${d.message}` : ""));
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="sshd 有效配置"
        note="来自 systemctl 与 sshd -T；修改不会在本机真实应用"
      >
        <div style={{ display: "flex", gap: 9, flexWrap: "wrap" }}>
          <Badge tone={ssh?.running ? "green" : "red"}>
            {ssh?.running ? "运行中" : "未运行"}
          </Badge>
          <Badge tone="blue">端口 {ssh?.port || "-"}</Badge>
          <Badge>Root {ssh?.permitRootLogin || "unknown"}</Badge>
          <Badge>密码 {ssh?.passwordAuthentication || "unknown"}</Badge>
          <Badge>公钥 {ssh?.pubkeyAuthentication || "unknown"}</Badge>
        </div>
        {ssh?.error && <ErrorBox error={ssh.error} />}
      </Section>
      <Section
        title="配置变更"
        note="保存前校验端口冲突并生成 sshd_config diff"
        actions={
          <button style={primary} onClick={() => run(false)}>
            生成预览
          </button>
        }
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))",
            gap: 9,
          }}
        >
          <Field label="端口">
            <input
              style={input}
              type="number"
              value={change.port}
              onChange={(e) => setChange({ ...change, port: +e.target.value })}
            />
          </Field>
          <Field label="Root 登录">
            <select
              style={input}
              value={change.permitRootLogin}
              onChange={(e) =>
                setChange({ ...change, permitRootLogin: e.target.value })
              }
            >
              <option value="no">禁止</option>
              <option value="prohibit-password">仅公钥</option>
              <option value="without-password">仅公钥（兼容值）</option>
              <option value="yes">允许</option>
            </select>
          </Field>
          <Field label="密码认证">
            <select
              style={input}
              value={change.passwordAuthentication}
              onChange={(e) =>
                setChange({ ...change, passwordAuthentication: e.target.value })
              }
            >
              <option value="no">关闭</option>
              <option value="yes">开启</option>
            </select>
          </Field>
        </div>
        <Preview text={preview} onConfirm={() => run(true)} />
      </Section>
      <ErrorBox error={error} />
    </div>
  );
}

function AccountTab({ settings, setSettings, reload }) {
  const [enroll, setEnroll] = useState(null);
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState([]);
  const [accessCode, setAccessCode] = useState("");
  const [settingsPreview, setSettingsPreview] = useState(null);
  const [error, setError] = useState("");
  const begin = async () => {
    try {
      setError("");
      setEnroll(
        await request("/api/v1/security/totp/enroll", {
          method: "POST",
          body: "{}",
        }),
      );
    } catch (e) {
      setError(e.message);
    }
  };
  const confirm = async () => {
    try {
      const d = await request("/api/v1/security/totp/confirm", {
        method: "POST",
        body: JSON.stringify({ code }),
      });
      setRecovery(d.recoveryCodes);
      setEnroll(null);
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  const save = async () => {
    try {
      const payload = settingsUpdatePayload(settings, { accessCode });
      const preview = await request("/api/v1/security/settings/preview", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      setSettingsPreview({ payload, preview });
    } catch (e) {
      setError(e.message);
    }
  };
  const confirmSave = async () => {
    try {
      await request("/api/v1/security/settings", {
        method: "POST",
        body: JSON.stringify({ ...settingsPreview.payload, confirm: true }),
      });
      setSettingsPreview(null);
      setAccessCode("");
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="登录前访问设置"
        note="访问码是登录密码前的额外共享口令，原值不会回显"
        actions={
          <button style={primary} onClick={save}>
            生成变更预览
          </button>
        }
      >
        <div
          style={{
            display: "flex",
            alignItems: "end",
            gap: 12,
            flexWrap: "wrap",
          }}
        >
          <label
            style={{
              fontSize: 12,
              color: T.ink2,
              display: "flex",
              alignItems: "center",
              gap: 7,
              height: 34,
            }}
          >
            <input
              type="checkbox"
              checked={!!settings.accessCodeEnabled}
              onChange={(e) =>
                setSettings({
                  ...settings,
                  accessCodeEnabled: e.target.checked,
                })
              }
            />
            启用访问码
          </label>
          <Field label="新访问码">
            <input
              type="password"
              autoComplete="new-password"
              style={input}
              value={accessCode}
              onChange={(e) => setAccessCode(e.target.value)}
              placeholder={
                settings.accessCodeConfigured
                  ? "已配置（留空不变）"
                  : "设置访问码"
              }
            />
          </Field>
        </div>
        <Preview
          text={settingsPreview?.preview}
          onConfirm={settingsPreview ? confirmSave : null}
          confirmLabel="确认保存"
        />
      </Section>
      <Section
        title="双重验证"
        note="启用后登录必须验证 TOTP 或一次性恢复码；TOTP 密钥经 AES-GCM 加密存储"
        actions={
          !settings.totpEnabled ? (
            <button style={primary} onClick={begin}>
              开始设置
            </button>
          ) : (
            <Badge tone="green">已启用</Badge>
          )
        }
      >
        {enroll && (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(220px,240px) minmax(0,1fr)",
              gap: 18,
              marginTop: 14,
              alignItems: "center",
            }}
          >
            <img
              src={enroll.qrDataURL}
              width="220"
              height="220"
              alt="TOTP 注册二维码"
              style={{
                border: `1px solid ${T.border}`,
                padding: 8,
                maxWidth: "100%",
                height: "auto",
              }}
            />
            <div>
              <div style={{ fontSize: 11, color: T.ink3 }}>手动密钥</div>
              <code
                style={{
                  display: "block",
                  margin: "6px 0 14px",
                  wordBreak: "break-all",
                  fontSize: 12,
                }}
              >
                {enroll.secret}
              </code>
              <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                <input
                  style={input}
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  placeholder="6 位验证码"
                />
                <button style={primary} onClick={confirm}>
                  验证并启用
                </button>
              </div>
            </div>
          </div>
        )}
        {recovery.length > 0 && (
          <Preview title="恢复码（仅显示一次）" text={recovery.join("\n")} />
        )}
      </Section>
      <ErrorBox error={error} />
    </div>
  );
}

function BansTab({ bans, reload }) {
  const [rule, setRule] = useState(
    bans?.rule || { threshold: 5, windowSec: 600, banMinutes: 30 },
  );
  const [rulePreview, setRulePreview] = useState(null);
  const [error, setError] = useState("");
  const save = async () => {
    try {
      setRulePreview(
        await request("/api/v1/security/ban-rule", {
          method: "POST",
          body: JSON.stringify(rule),
        }),
      );
    } catch (e) {
      setError(e.message);
    }
  };
  const confirmSave = async () => {
    try {
      await request("/api/v1/security/ban-rule", {
        method: "PUT",
        body: JSON.stringify({ ...rule, confirm: true }),
      });
      setRulePreview(null);
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  const unban = async (ip) => {
    try {
      await request(`/api/v1/security/bans/${encodeURIComponent(ip)}`, {
        method: "DELETE",
      });
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="自动封禁规则"
        note="作用于 DevBox 登录；SSH 日志监控当前仅展示状态"
        actions={
          <button style={primary} onClick={save}>
            生成规则预览
          </button>
        }
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))",
            gap: 9,
          }}
        >
          <Field label="失败次数">
            <input
              style={input}
              type="number"
              value={rule.threshold}
              onChange={(e) => setRule({ ...rule, threshold: +e.target.value })}
            />
          </Field>
          <Field label="时间窗（秒）">
            <input
              style={input}
              type="number"
              value={rule.windowSec}
              onChange={(e) => setRule({ ...rule, windowSec: +e.target.value })}
            />
          </Field>
          <Field label="封禁（分钟）">
            <input
              style={input}
              type="number"
              value={rule.banMinutes}
              onChange={(e) =>
                setRule({ ...rule, banMinutes: +e.target.value })
              }
            />
          </Field>
          <Field label="保护网段（逗号分隔）">
            <input
              style={input}
              value={(rule.protectedCIDRs || ["10.126.126.0/24"]).join(", ")}
              onChange={(e) =>
                setRule({
                  ...rule,
                  protectedCIDRs: e.target.value
                    .split(",")
                    .map((value) => value.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
        </div>
        <Preview
          text={rulePreview}
          onConfirm={rulePreview ? confirmSave : null}
          confirmLabel="确认保存规则"
        />
      </Section>
      <Section
        title="封禁列表"
        note={`SSH 日志监控：${bans?.sshLogMonitoring === "display-only" ? "仅展示，未启用" : "-"}`}
      >
        {bans?.items?.length ? (
          <div style={{ display: "grid", gap: 7 }}>
            {bans.items.map((b) => (
              <div
                key={b.ip}
                style={{
                  display: "flex",
                  alignItems: "center",
                  padding: "9px 10px",
                  border: `1px solid ${T.borderSoft}`,
                  borderRadius: 6,
                  flexWrap: "wrap",
                  gap: 8,
                }}
              >
                <code>{b.ip}</code>
                <span style={{ fontSize: 11.5, color: T.ink3 }}>
                  失败 {b.failures} 次 · 至 {new Date(b.until).toLocaleString()}
                </span>
                <div style={{ flex: 1 }} />
                <button style={danger} onClick={() => unban(b.ip)}>
                  解封
                </button>
              </div>
            ))}
          </div>
        ) : (
          <Empty text="当前没有被封禁的 IP" />
        )}
      </Section>
      <ErrorBox error={error} />
    </div>
  );
}

function FirewallTab({ firewall, remote }) {
  const session = remote?.currentSessionIP || "";
  const initialRules = session
    ? [
        {
          direction: "in",
          action: "allow",
          protocol: "any",
          port: 0,
          source: "any",
          interface: "tun0",
          comment: "保留远程管理隧道",
        },
        {
          direction: "in",
          action: "allow",
          protocol: "any",
          port: 0,
          source: session,
          interface: "",
          comment: "保留当前会话",
        },
      ]
    : [];
  const [rules, setRules] = useState(initialRules);
  const [preview, setPreview] = useState("");
  const [error, setError] = useState("");
  const add = () =>
    setRules([
      ...rules,
      {
        direction: "in",
        action: "allow",
        protocol: "tcp",
        port: 443,
        source: "any",
        interface: "",
        comment: "",
      },
    ]);
  const update = (i, k, v) =>
    setRules(rules.map((r, n) => (n === i ? { ...r, [k]: v } : r)));
  const run = async (apply = false) => {
    try {
      setError("");
      const d = await request(
        `/api/v1/security/firewall/${apply ? "apply" : "preview"}`,
        {
          method: "POST",
          body: JSON.stringify({ rules, sessionIP: session, confirm: apply }),
        },
      );
      setPreview(apply ? `${d.preview.ruleset}\n${d.message}` : d.ruleset);
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="当前系统规则"
        note={`${firewall?.backend || "unavailable"} · 只读；输出与 nft list ruleset / iptables-save 一致`}
      >
        <pre
          style={{
            margin: 0,
            maxHeight: 190,
            overflow: "auto",
            background: "#0f172a",
            color: "#cbd5e1",
            padding: 11,
            borderRadius: 6,
            fontSize: 10.5,
            whiteSpace: "pre-wrap",
          }}
        >
          {firewall?.ruleset || firewall?.error || "无可读取规则"}
        </pre>
      </Section>
      <Section
        title="规则编辑器"
        note={`防锁死保护：tun0 + 当前会话 ${session || "未知"}；确认后仍为 dry-run`}
        actions={
          <>
            <button style={button} onClick={add}>
              <Icon name="plus" size={13} />
              添加
            </button>
            <button style={primary} onClick={() => run(false)}>
              生成预览
            </button>
          </>
        }
      >
        <div style={{ display: "grid", gap: 6, overflowX: "auto" }}>
          {rules.map((r, i) => (
            <div
              key={i}
              style={{
                display: "grid",
                gridTemplateColumns:
                  "90px 90px 90px 100px minmax(150px,1fr) 110px minmax(160px,1.3fr) 30px",
                gap: 6,
                minWidth: 850,
              }}
            >
              <select
                style={input}
                value={r.direction}
                onChange={(e) => update(i, "direction", e.target.value)}
              >
                <option value="in">入站</option>
                <option value="out">出站</option>
              </select>
              <select
                style={input}
                value={r.action}
                onChange={(e) => update(i, "action", e.target.value)}
              >
                <option value="allow">允许</option>
                <option value="deny">拒绝</option>
              </select>
              <select
                style={input}
                value={r.protocol}
                onChange={(e) => update(i, "protocol", e.target.value)}
              >
                <option value="any">任意</option>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
              <input
                style={input}
                type="number"
                value={r.port || 0}
                onChange={(e) => update(i, "port", +e.target.value)}
              />
              <input
                style={input}
                value={r.source}
                onChange={(e) => update(i, "source", e.target.value)}
                placeholder="源地址"
              />
              <input
                style={input}
                value={r.interface}
                onChange={(e) => update(i, "interface", e.target.value)}
                placeholder="接口"
              />
              <input
                style={input}
                value={r.comment}
                onChange={(e) => update(i, "comment", e.target.value)}
                placeholder="说明"
              />
              <button
                title="删除规则"
                style={{
                  ...danger,
                  width: 30,
                  padding: 0,
                  justifyContent: "center",
                }}
                onClick={() => setRules(rules.filter((_, n) => n !== i))}
              >
                ×
              </button>
            </div>
          ))}
        </div>
        <Preview
          text={preview}
          onConfirm={() => run(true)}
          confirmLabel="二次确认 dry-run"
        />
      </Section>
      <ErrorBox error={error} />
    </div>
  );
}

function CertsTab({ certs, reload, settings, setSettings }) {
  const [form, setForm] = useState({
    name: "devbox-self",
    hosts: "devbox.local",
    validDays: 365,
  });
  const [upload, setUpload] = useState({
    name: "",
    certificate: "",
    privateKey: "",
  });
  const [preview, setPreview] = useState(null);
  const [error, setError] = useState("");
  const selfPayload = () => ({
    name: form.name,
    hosts: form.hosts
      .split(",")
      .map((x) => x.trim())
      .filter(Boolean),
    validDays: +form.validDays,
  });
  const generate = async () => {
    try {
      const payload = selfPayload();
      setPreview({
        kind: "self",
        payload,
        data: await request("/api/v1/security/certificates/self-signed", {
          method: "POST",
          body: JSON.stringify(payload),
        }),
      });
    } catch (e) {
      setError(e.message);
    }
  };
  const send = async () => {
    try {
      setPreview({
        kind: "upload",
        payload: upload,
        data: await request("/api/v1/security/certificates/preview", {
          method: "POST",
          body: JSON.stringify(upload),
        }),
      });
    } catch (e) {
      setError(e.message);
    }
  };
  const bind = async () => {
    try {
      const payload = settingsUpdatePayload(settings);
      setPreview({
        kind: "bind",
        payload,
        data: await request("/api/v1/security/settings/preview", {
          method: "POST",
          body: JSON.stringify(payload),
        }),
      });
    } catch (e) {
      setError(e.message);
    }
  };
  const confirm = async () => {
    try {
      if (preview.kind === "self")
        await request("/api/v1/security/certificates/self-signed", {
          method: "POST",
          body: JSON.stringify({ ...preview.payload, confirm: true }),
        });
      if (preview.kind === "upload")
        await request("/api/v1/security/certificates", {
          method: "POST",
          body: JSON.stringify({ ...preview.payload, confirm: true }),
        });
      if (preview.kind === "bind")
        await request("/api/v1/security/settings", {
          method: "POST",
          body: JSON.stringify({ ...preview.payload, confirm: true }),
        });
      setPreview(null);
      if (preview.kind === "upload")
        setUpload({ name: "", certificate: "", privateKey: "" });
      reload();
    } catch (e) {
      setError(e.message);
    }
  };
  return (
    <div style={{ display: "grid", gap: 12 }}>
      <Section
        title="证书列表"
        note="私钥永不返回前端；30 天内到期显示告警"
        actions={
          <button style={primary} onClick={bind}>
            预览 HTTPS 绑定
          </button>
        }
      >
        <div style={{ display: "grid", gap: 7 }}>
          {certs?.items?.map((c) => (
            <label
              key={c.name}
              style={{
                display: "flex",
                alignItems: "center",
                padding: 10,
                border: `1px solid ${T.borderSoft}`,
                borderRadius: 6,
              }}
            >
              <input
                type="radio"
                name="cert"
                checked={settings.httpsCertificate === c.name}
                onChange={() =>
                  setSettings({ ...settings, httpsCertificate: c.name })
                }
              />
              <div style={{ marginLeft: 9 }}>
                <div style={{ fontSize: 12.5, fontWeight: 650 }}>
                  {c.name} {c.selfSigned && <Badge>自签</Badge>}{" "}
                  {c.expiring && <Badge tone="amber">临近到期</Badge>}
                </div>
                <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 3 }}>
                  {c.subject} · {new Date(c.notAfter).toLocaleDateString()} ·{" "}
                  {c.daysLeft} 天
                </div>
              </div>
            </label>
          ))}
          {!certs?.items?.length && <Empty text="尚未安装证书" />}
        </div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 10 }}>
          ACME 自动续签：占位说明，本期未实现。
        </div>
        {preview?.kind === "bind" && (
          <Preview
            text={preview.data}
            onConfirm={confirm}
            confirmLabel="确认绑定并重启后生效"
          />
        )}
      </Section>
      <Section
        title="生成自签证书"
        actions={
          <button style={primary} onClick={generate}>
            生成预览
          </button>
        }
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))",
            gap: 9,
          }}
        >
          <Field label="名称">
            <input
              style={input}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="域名/IP（逗号分隔）">
            <input
              style={input}
              value={form.hosts}
              onChange={(e) => setForm({ ...form, hosts: e.target.value })}
            />
          </Field>
          <Field label="有效天数">
            <input
              style={input}
              type="number"
              value={form.validDays}
              onChange={(e) => setForm({ ...form, validDays: e.target.value })}
            />
          </Field>
        </div>
        {preview?.kind === "self" && (
          <Preview
            text={preview.data}
            onConfirm={confirm}
            confirmLabel="确认生成"
          />
        )}
      </Section>
      <Section
        title="上传 PEM 证书"
        note="提交前解析有效期并校验证书与 RSA 私钥匹配"
        actions={
          <button style={primary} onClick={send}>
            校验并预览
          </button>
        }
      >
        <Field label="名称">
          <input
            style={{ ...input, width: "min(240px,100%)" }}
            value={upload.name}
            onChange={(e) => setUpload({ ...upload, name: e.target.value })}
          />
        </Field>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit,minmax(240px,1fr))",
            gap: 9,
            marginTop: 9,
          }}
        >
          <textarea
            rows={7}
            style={{
              ...input,
              height: "auto",
              padding: 9,
              fontFamily: "monospace",
            }}
            value={upload.certificate}
            onChange={(e) =>
              setUpload({ ...upload, certificate: e.target.value })
            }
            placeholder="-----BEGIN CERTIFICATE-----"
          />
          <textarea
            rows={7}
            style={{
              ...input,
              height: "auto",
              padding: 9,
              fontFamily: "monospace",
            }}
            value={upload.privateKey}
            onChange={(e) =>
              setUpload({ ...upload, privateKey: e.target.value })
            }
            placeholder="-----BEGIN PRIVATE KEY-----"
          />
        </div>
        {preview?.kind === "upload" && (
          <Preview
            text={preview.data}
            onConfirm={confirm}
            confirmLabel="确认上传"
          />
        )}
      </Section>
      <ErrorBox error={error} />
    </div>
  );
}

function DiagnosticsTab() {
  const [running, setRunning] = useState(null);
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit,minmax(230px,1fr))",
        gap: 12,
      }}
    >
      {diagnosticsTools.map((tool) => (
        <button
          key={tool.id}
          type="button"
          onClick={() => setRunning(tool.id)}
          style={{
            ...panel,
            minHeight: 92,
            display: "flex",
            alignItems: "center",
            gap: 12,
            textAlign: "left",
            cursor: "pointer",
            borderColor: running === tool.id ? tool.color : T.border,
            boxShadow: running === tool.id ? `0 0 0 2px ${tool.color}22` : "none",
          }}
        >
          <span
            style={{
              width: 38,
              height: 38,
              borderRadius: 7,
              flexShrink: 0,
              display: "grid",
              placeItems: "center",
              color: tool.color,
              background: `${tool.color}15`,
            }}
          >
            <Icon name={tool.icon} size={18} />
          </span>
          <span style={{ minWidth: 0 }}>
            <span style={{ display: "block", color: T.ink, fontSize: 13, fontWeight: 700 }}>
              {tool.name}
            </span>
            <span style={{ display: "block", color: T.ink3, fontSize: 11.5, marginTop: 4 }}>
              {tool.desc}
            </span>
            {running === tool.id && (
              <span style={{ display: "block", color: tool.color, fontSize: 11.5, fontWeight: 650, marginTop: 7 }}>
                正在执行...
              </span>
            )}
          </span>
        </button>
      ))}
    </div>
  );
}

export default function NetworkSecuritySettings() {
  const [tab, setTab] = useState("network");
  const [data, setData] = useState({});
  const [settings, setSettings] = useState({});
  const [error, setError] = useState("");
  const load = async () => {
    try {
      setError("");
      const [network, remote, security, ssh, bans, firewall, certs] =
        await Promise.all(
          [
            "/api/v1/network",
            "/api/v1/network/remote-access",
            "/api/v1/security/settings",
            "/api/v1/security/ssh",
            "/api/v1/security/bans",
            "/api/v1/security/firewall",
            "/api/v1/security/certificates",
          ].map((x) => request(x)),
        );
      setData({ network, remote, ssh, bans, firewall, certs });
      setSettings(security);
    } catch (e) {
      setError(e.message);
    }
  };
  useEffect(() => {
    const first = setTimeout(load, 0);
    const id = setInterval(() => {
      if (tab === "network") load();
    }, 5000);
    return () => {
      clearTimeout(first);
      clearInterval(id);
    };
  }, [tab]);
  const body = {
    network: <NetworkTab network={data.network} />,
    remote: (
      <RemoteTab
        remote={data.remote}
        settings={settings}
        setSettings={setSettings}
        reload={load}
      />
    ),
    ssh: <SSHTab key={data.ssh?.port || "ssh"} ssh={data.ssh} />,
    account: (
      <AccountTab settings={settings} setSettings={setSettings} reload={load} />
    ),
    bans: (
      <BansTab
        key={JSON.stringify(data.bans?.rule || {})}
        bans={data.bans}
        reload={load}
      />
    ),
    firewall: (
      <FirewallTab
        key={data.remote?.currentSessionIP || "firewall"}
        firewall={data.firewall}
        remote={data.remote}
      />
    ),
    certs: (
      <CertsTab
        certs={data.certs}
        reload={load}
        settings={settings}
        setSettings={setSettings}
      />
    ),
    diagnostics: <DiagnosticsTab />,
  }[tab];
  return (
    <div
      style={{
        flex: 1,
        minWidth: 0,
        display: "flex",
        flexDirection: "column",
        background: T.surfaceAlt,
        overflow: "hidden",
      }}
    >
      <header
        style={{
          background: T.surface,
          borderBottom: `1px solid ${T.border}`,
          padding: "14px 20px 0",
          minWidth: 0,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", minWidth: 0 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 750, color: T.ink }}>
              网络与安全
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              实时状态、远程入口与访问保护
            </div>
          </div>
          <div style={{ flex: 1 }} />
          <div style={{ flexShrink: 0 }}>
            <Badge tone="green">只读采集已连接</Badge>
          </div>
        </div>
        <nav
          style={{
            display: "flex",
            gap: 2,
            marginTop: 12,
            overflowX: "auto",
            maxWidth: "100%",
          }}
        >
          {tabs.map(([id, label, icon]) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              style={{
                height: 36,
                padding: "0 12px",
                border: 0,
                borderBottom: `2px solid ${tab === id ? T.blueDeep : "transparent"}`,
                background: "transparent",
                color: tab === id ? T.blueDeep : T.ink3,
                fontSize: 12,
                fontWeight: tab === id ? 700 : 550,
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                gap: 6,
                flexShrink: 0,
                whiteSpace: "nowrap",
              }}
            >
              <Icon name={icon} size={13} />
              {label}
            </button>
          ))}
        </nav>
      </header>
      <main style={{ flex: 1, minWidth: 0, overflow: "auto", padding: 16 }}>
        <ErrorBox error={error} />
        {body}
      </main>
    </div>
  );
}
