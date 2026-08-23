# DevBox 资源与日志 API

Issue #18 在现有监控、进程、Supervisor 和审计页面上补齐资源管理与系统日志能力。所有 `/api/v1/*` 接口继续由控制台 Bearer session 保护。

## 指标与硬盘

### `GET /api/v1/metrics`

返回当前 CPU、内存、网络累计字节、文件系统容量和物理盘 I/O。`diskIO[]` 新增：

- `temperatureC`: sysfs 提供硬盘温度时返回；不可检测时省略。
- `temperatureStatus`: `available` 或 `unsupported`。调用方不得把 `unsupported` 渲染为 `0 °C`。
- `readBytesPerSec`、`writeBytesPerSec`、`utilPercent`: 最近一个采样窗口的真实差分值。

### `GET /api/v1/metrics/history?window=1h`

`window` 支持 Go duration 格式，UI 使用 `1h`、`6h`、`24h`。采集器保留 24 小时数据，响应最多均匀下采样到 720 点，并始终保留最新点。

### `GET /api/v1/disks`

返回 `lsblk` 物理盘、容量、分区、挂载点和系统盘标识。资源管理硬盘视图将它与 `/metrics` 的实时 I/O 合并展示用途、容量、温度、繁忙度和吞吐。

## 进程与服务

### `GET /api/v1/processes`

在原有进程字段上新增：

| 字段 | 含义 |
| --- | --- |
| `cpuPercent` | 两次 API 采样间的 CPU 使用率；第一次为 `null`（采样中） |
| `cpuTimeSeconds` | 进程累计 CPU 时间 |
| `runtimeSeconds` | 进程运行时间 |
| `startTicks` | Linux `/proc/<pid>/stat` 启动 tick；终止请求必须原样回传，用于确认 PID 身份 |
| `readBps` / `writeBps` | `/proc/<pid>/io` 可读时的磁盘速率；首次为 `null` |
| `ioStatus` | `available` 或 `unavailable` |
| `ports` | 该 PID 的监听端口 |
| `portsStatus` | `available` 或 `unavailable` |

### `POST /api/v1/processes/{pid}/terminate`

向进程发送 `SIGTERM`。请求 JSON 为 `{ "startTicks": 12345 }`，其中值来自最近一次进程列表。此危险操作有独立于全局中间件的权限校验：必须启用控制台密码认证并携带有效 Bearer token。

服务端先持久化 `outcome: "intent"`，再用 `pidfd_open` 打开稳定进程句柄，并校验句柄打开后的 `/proc/<pid>/stat` starttime 与请求身份一致，最后通过 `pidfd_send_signal` 发送信号。PID 1、DevBox 自身及父进程、内核线程受保护。

- `202`: 已发送 `SIGTERM`。
- `401`: token 无效。
- `403 permission_required`: 未启用控制台密码认证。
- `403 protected_process`: 系统关键进程。
- `409 process_identity_changed`: PID 已复用或请求身份已过期，未发送信号。
- `400 process_identity_required`: 请求未携带有效 `startTicks`。
- `404 process_not_found`: PID 不存在或在操作前退出。
- `403 permission_denied`: OS 拒绝发信号。
- `503 audit_unavailable`: 审计 intent 无法持久化，危险操作未执行。

认证失败不写终止审计，避免未认证请求刷日志。token 验证成功后，受保护 PID、PID 不存在、身份变化、信号成功或失败等所有路径都保留 intent 与结果事件。UI 在请求前显示二次确认。

### `GET /api/v1/supervisor/resources`

返回 Supervisor 服务及其 CPU、CPU 时间、运行时长、内存、磁盘读写和监听端口。Linux 没有通用的逐进程网络字节计数，因此 `networkStatus` 明确返回 `unsupported`，不伪造为 0。

前端将可检测到的零磁盘吞吐显示为 `0 B/s`；首次采样、读取失败和平台不支持分别显示“采样中”“无数据”和“不支持”，避免把缺失指标误写成 0，也避免把真实零值误写成缺失。

## 系统日志

系统事件和操作审计统一存储为 `/var/lib/devbox/system-events.jsonl`，文件权限为 `0600`；开发和测试环境可用 `DEVBOX_SYSTEM_LOG_PATH` 覆盖路径。存储初始化失败时，终止进程和清空日志端点返回 503 并保持禁用。登录成功/失败、Supervisor 服务控制、应用安装/启停/卸载、终止进程和清空日志都写入该存储。

payload 会递归处理任意 map、struct、slice 和 pointer，敏感 key 与内联 `key=value` 均会脱敏；`Authorization:`、`Cookie:`、`Set-Cookie:` header 的完整值会替换为 `[REDACTED]`。当前认证模型为单管理员共享密码，成功 session 的审计身份由服务端固定为 `admin`，不接受客户端 username 作为可信身份。

应用异步写操作被 controller 接受时记录 `outcome: "accepted"` 和 `task_id`；worker 将任务写入 `succeeded` 或 `failed` 终态后，再分别记录 `success` 或 `failure` 事件。

### `GET /api/v1/audit/events`

查询参数：

| 参数 | 说明 |
| --- | --- |
| `level` | 可重复；`info`、`warning`、`error` |
| `module` | 可重复；例如 `auth`、`supervisor`、`apps`、`process`、`audit` |
| `user` | 用户名模糊匹配 |
| `since` / `until` | RFC3339 时间 |
| `limit` | 1-200，默认 50 |
| `offset` | 从 0 开始的偏移 |

响应包含 `events`、`total`、`limit`、`offset`。事件字段包括 `level`、`module`、`ts`、`username`、`event`，并保留兼容审计字段 `event_type`、`outcome`、资源和来源信息。

### `DELETE /api/v1/audit/events`

清空现有日志。要求已启用密码认证和有效 Bearer token。服务端先同步落盘 `outcome: "intent"`；失败时返回 503 且不清空。成功时使用同目录原子替换，并保留 intent 与 `outcome: "success"` 两条 `LOG_CLEAR` 事件，因此清空动作及其结果都不会消失。

## 审计来源与会话

来源 IP 默认取 TCP peer 的 `RemoteAddr`，不信任 `X-Forwarded-For`。只有 peer 命中 `console.trusted_proxies` 配置的 IP/CIDR 时，才从 XFF 链右向左剥离可信代理并记录首个不可信 hop。token 过期或调用 `POST /api/v1/auth/logout` 时，服务器同步删除 token 对应的审计身份绑定。

## 前端采样策略

资源页、服务/进程页、Supervisor、硬件传感器、网络连接和日志页共用可见性轮询策略：页面可见时按各自间隔刷新；`document.hidden` 时清除 interval；重新可见时立即刷新并恢复 interval。行为由 `console-ui/src/lib/visiblePolling.test.js` 断言。
