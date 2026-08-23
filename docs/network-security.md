# 网络、远程访问与安全配置

Issue #13 为 DevBox 增加真实的主机网络状态、远程入口和应用层安全设置。系统设置中的数据来自本机命令与文件，不使用静态演示数据。

## 数据来源

| 能力 | 数据来源 | 写入策略 |
| --- | --- | --- |
| 网口 | `ip -j addr show`、`ip -j route show default`、`/sys/class/net`、`/etc/resolv.conf` | 只读 |
| 监听入口 | `ss -H -lntp`、tun0 地址 | 只读 |
| SSH | `systemctl is-active sshd/ssh`、`sshd -T` | diff 预览；本机不 apply |
| 防火墙 | `nft list ruleset`，失败后读取 `iptables-save` | 规则预览；本机不 apply |
| 证书 | 服务端证书目录中的 PEM | 上传、自签和绑定配置；不改系统证书库 |

网口采集器对物理、无线、隧道、虚拟、回环接口分类展示。实时速率按两次 `/sys/class/net/<iface>/statistics/{rx,tx}_bytes` 采样差计算。地址、端口和端口占用在预览阶段校验。

## 持久化和敏感数据

动态设置默认写入 `/var/lib/devbox/security/settings.json`，主密钥写入 `/var/lib/devbox/security/master.key`。目录可通过 `DEVBOX_SECURITY_DATA_DIR` 覆盖。

- TOTP 密钥使用随机 256 位主密钥和 AES-GCM 加密后落盘。
- 访问码最少 8 个字符；访问码和恢复码使用低 cost bcrypt 保存，以兼顾抗离线破解与本机登录延迟。
- 恢复码由 10 字节随机数编码为 16 个 Base32 字符（80 bit），验证成功后只有在持久化删除成功时才允许登录。
- DDNS 只保存凭据引用，例如 `env:CLOUDFLARE_TOKEN`，不保存凭据明文。
- 私钥以 `0600` 保存，证书 API 只返回元数据，绝不返回私钥。
- API 响应不会返回访问码、TOTP 密钥（注册时一次性返回除外）、凭据原值或私钥。

## 登录保护

登录顺序为访问码、登录密码、TOTP/恢复码。TOTP 注册流程是：

1. `POST /api/v1/security/totp/enroll` 返回二维码、`otpauth://` URI 和一次性展示的手动密钥。
2. 用户用认证器生成动态码，再调用 `POST /api/v1/security/totp/confirm`。
3. 服务端验证成功后启用 TOTP，并返回十个只显示一次的恢复码。
4. 登录接口在签发 session 前验证动态码；恢复码只能使用一次。

登录失败按来源 IP 记录。默认 10 分钟内失败 5 次，封禁 30 分钟。规则可编辑，封禁可手动解除。当前 SSH 日志监控只展示占位状态，不会修改 SSH 或系统封禁表。

启用 TOTP 后每次登录都必须提交 TOTP 或恢复码，API 不再提供独立的 `forceTwoFactor` 开关。即使没有配置本地密码，只要访问码或 TOTP 任一因子启用，API 也要求真实 session token。session 只在所有因子验证成功后创建，创建新 session 和校验旧 token 时会清扫过期项。

TOTP 登录会记录已消费的 `(timestep, code)`，同一动态码在允许时间窗口内不能重复使用。消费记录只保存在进程内存中：不把动态码派生数据写入磁盘，代价是服务重启后当前时间窗内的旧动态码可能再次使用；窗口最长约 90 秒。

封禁管理器默认保护 `10.126.126.0/24`，也支持替换为配置的管理网段；当前管理会话 IP 只记录告警，不进入封禁表。

## 防锁死设计

防火墙预览必须同时包含：

- 入站允许 `tun0` 的保护规则；
- 入站允许当前 HTTP 会话来源 IP 的保护规则。

两条保护规则都必须是不受协议端口限制的入站 allow，并位于任何能匹配当前管理流量的 deny 之前。tun0 规则不能增加 Source 收窄条件；会话保护规则指定网口时，该网口必须存在于当前采集结果。任一规则缺失、被遮蔽、范围过窄或引用伪接口，服务端拒绝生成预览。预览还携带 60 秒定时回滚方案说明。确认端点需要二次确认，但当前构建固定返回 `dryRun: true` 和 `not-applied`，不会执行 `nft`、`iptables` 或 SSH 配置命令。

## DDNS、外链和限速

DDNS 提供 Cloudflare 和自定义 Webhook 模型。“立即更新”当前只支持 dry-run；用于命令验证时仅允许 `echo`/`/bin/echo`，其他命令会被拒绝。

分享域名作为 #11 文件外链的 URL 基线配置。全局上传和下载限速在 HTTP 中间件执行，值为字节/秒，`0` 表示不限速。HTTP/HTTPS 端口及证书绑定保存后提示重启生效。

## 证书

上传接口校验 PEM、X.509 有效期以及 RSA 证书与私钥是否匹配。列表展示主题、SAN、有效期、自签状态和 30 天临过期告警。支持生成 RSA 2048 自签证书。ACME 自动续签仅保留说明，本期不实现。

## 本机验证边界

允许真实执行的操作只有状态读取、应用层登录保护、应用层限速、DevBox 私有安全设置持久化、DevBox 私有证书目录读写，以及 DDNS dry-run/`echo` 验证。

以下操作在开发机上禁止真实执行：网口配置、SSH 配置写入或重启、防火墙规则 apply/禁用/重载、HTTP/HTTPS 监听端口热切换。相关 UI 到“预览待执行变更”和 dry-run 确认结果为止。
