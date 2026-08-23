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
| `readBps` / `writeBps` | `/proc/<pid>/io` 可读时的磁盘速率；首次为 `null` |
| `ioStatus` | `available` 或 `unavailable` |
| `ports` | 该 PID 的监听端口 |
| `portsStatus` | `available` 或 `unavailable` |

### `POST /api/v1/processes/{pid}/terminate`

向进程发送 `SIGTERM`。此危险操作有独立于全局中间件的权限校验：必须启用控制台密码认证并携带有效 Bearer token。PID 1 和 DevBox 自身受保护。

- `202`: 已发送 `SIGTERM`。
- `401`: token 无效。
- `403 permission_required`: 未启用控制台密码认证。
- `403 protected_process`: 系统关键进程。
- `404 process_not_found`: PID 不存在或在操作前退出。
- `403 permission_denied`: OS 拒绝发信号。

成功或失败的终止尝试都会写入结构化日志。UI 在请求前显示二次确认。

### `GET /api/v1/supervisor/resources`

返回 Supervisor 服务及其 CPU、CPU 时间、运行时长、内存、磁盘读写和监听端口。Linux 没有通用的逐进程网络字节计数，因此 `networkStatus` 明确返回 `unsupported`，不伪造为 0。

前端将可检测到的零磁盘吞吐显示为 `0 B/s`；首次采样、读取失败和平台不支持分别显示“采样中”“无数据”和“不支持”，避免把缺失指标误写成 0，也避免把真实零值误写成缺失。

## 系统日志

系统事件和操作审计统一存储为 `/var/lib/devbox/system-events.jsonl`，文件权限为 `0600`；开发和测试环境可用 `DEVBOX_SYSTEM_LOG_PATH` 覆盖路径。登录成功/失败、Supervisor 服务控制、应用安装/启停/卸载、终止进程和清空日志都写入该存储。payload 在写入前按敏感 key 和内联 `key=value` 模式脱敏。

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

清空现有日志。要求已启用密码认证和有效 Bearer token。清空使用同目录原子替换，完成后立即写入唯一的 `LOG_CLEAR` 审计事件，因此清空动作本身不会消失。

## 前端采样策略

资源页、服务/进程页、Supervisor、硬件传感器、网络连接和日志页共用可见性轮询策略：页面可见时按各自间隔刷新；`document.hidden` 时清除 interval；重新可见时立即刷新并恢复 interval。行为由 `console-ui/src/lib/visiblePolling.test.js` 断言。
