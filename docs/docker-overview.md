# Docker 服务概览与运行配置

Docker 桌面应用提供宿主机 Docker 的首屏状态、实时资源监控和运行配置入口。Compose 项目深层管理继续由「Compose 应用」负责；两者复用 `pkg/apps` 中的 Docker Engine 客户端、controller 装配、鉴权边界和领域错误信封。

## 状态事实源

- 服务状态以 Docker Engine `/info` 为最终依据。失败时区分 `permission_denied`、`timeout`、`unreachable`；只有 systemd/service 明确 inactive 时才显示 `stopped`，并保留诊断。
- 容器运行数与总数来自 `/info` 的 `ContainersRunning` 和 `Containers`。
- Compose 项目数从 `/containers/json?all=1` 的 `com.docker.compose.project` 标签去重，和 Compose 应用发现使用相同 Engine API。
- `data-root` 运行时优先展示 `/info.DockerRootDir`；daemon 不可用时读取 `/etc/docker/daemon.json`。未显式配置时仍展示 Docker 默认值 `/var/lib/docker`，但 `configured=false`，停止状态下必须先从存储设置入口确认位置才能启动。已运行 daemon 不会因此被停止或隐藏。
- daemon 不可用属于正常空态。概览和 stats 返回 200 与诊断信息，不让前端轮询产生白屏或持续 5xx。

## API

| 方法 | 路径 | 行为 |
|---|---|---|
| `GET` | `/api/v1/docker/overview` | 服务、Compose 项目、容器、data-root 与磁盘容量 |
| `GET` | `/api/v1/docker/stats` | 聚合运行容器的 CPU、内存、网络累计字节 |
| `POST` | `/api/v1/docker/service` | `{"action":"start|stop|restart"}`；操作后重新查询 daemon |
| `PUT` | `/api/v1/docker/autostart` | `{"enabled":true|false}`；使用 `systemctl enable/disable` 并复查 |
| `POST` | `/api/v1/docker/storage/plan` | 校验目标、容量与当前配置，生成迁移计划和计划指纹 |
| `POST` | `/api/v1/docker/storage/execute` | 携带 `targetPath`、最新 `planId` 和 `confirm:true` 执行迁移 |

所有接口位于 console 现有 `/api/v1/*` 认证中间件之后。写操作沿用应用管理错误信封：

```json
{
  "error": "Docker 服务操作失败",
  "reason": "permission_denied",
  "detail": "journalctl 或命令诊断摘要"
}
```

常见 `reason` 包括 `permission_denied`、`service_control_unsupported`、`storage_invalid`、`migration_plan_changed` 和 `migration_start_failed`。

## 服务控制

Linux 主机优先使用 systemd；没有 systemd 时回退 `service docker start|stop|restart`。开机自启只在 systemd 环境开放。无服务管理器、服务未安装或调用用户权限不足时，API 返回明确的 capability 错误。配置为 TCP/HTTP 远程 daemon 时只保留 Engine 概览，服务控制、开机自启与存储迁移统一禁用，绝不操作本机 systemd、daemon.json 或数据目录。

启停接口不会乐观更新状态。命令返回后后端最多等待 5 秒轮询 Engine API；状态不符合预期时返回 `service_state_mismatch`。启动失败时附带 `journalctl -u docker.service --no-pager -n 30` 摘要（最多 4 KiB）。

Docker 存储路径未显式确认、无效、不是绝对路径、daemon 配置无法解析或磁盘容量无法读取时，启动入口被阻止并提示先设置存储位置。

迁移持有 manager 级主机维护写锁；迁移期间 `start`、`restart`、`stop` 会立即返回 `migration_in_progress`，不会等待或与迁移命令交错。

## 存储迁移

生成计划是只读操作，包含：

1. 当前与目标 data-root。
2. 当前目录实际文件大小和目标磁盘可用容量。
3. 保留其它键、只修改 `data-root` 的完整 `daemon.json` 预览。
4. 停止服务、`rsync -aHAX --numeric-ids` 复制、原子写配置、启动并核对 `DockerRootDir` 的步骤。

目标路径必须是绝对路径、与当前路径不能互相包含，且已存在时必须为空。解析符号链接后的 canonical 路径必须位于 `compose.migration_allowed_roots` 白名单内（默认 `/data`、`/var/lib`），并始终拒绝 `/run`、`/etc`、`/proc`、`/sys`、`/dev`。执行请求必须携带刚生成的计划 ID 和 `confirm:true`；源路径、目标路径或配置变化会使计划指纹失效，调用方必须重新生成并确认。

执行时用 `O_DIRECTORY|O_NOFOLLOW` 打开 canonical 目标并在同一 fd 上复查空目录；该 fd 作为子进程 fd 3 一直持有到 rsync 完成，rsync 目的固定为 `/proc/self/fd/3/`，路径被替换为 symlink 也不会改变复制目标。确认 daemon 已停止后才复制数据。复制或写配置失败会用旧配置重启；新 data-root 启动或验证失败会恢复原 `daemon.json` 并尝试重启。迁移成功后不自动删除旧 data-root，避免把验证与不可恢复清理绑定在同一步。

生产主机不得通过 UI 迁移来做常规验收。迁移与启停路径使用注入的命令、服务和存储 mock 做单元测试；只读概览和 stats 才适合直接对照真实 Docker daemon。

## 实时监控

概览从第一次主机/Engine 探测起共享 8 秒总预算，Engine HTTP 客户端超时同为 8 秒。stats 接口从容器列表请求前创建 5 秒总预算，只查询运行容器，最多并发 8 个 `/containers/{id}/stats?stream=false` 请求，避免慢 daemon 造成轮询请求堆积。CPU 使用率按 Docker 的容器 delta 公式计算后求和；内存 usage 求和并以 `/info.MemTotal` 作为宿主容量，各网络接口收发累计字节分别求和。单个容器采样失败不会使整次响应失败，响应会报告 `failedContainers` 和诊断；全部容器超时时返回明确监控空态。

前台页面每 3 秒取 stats、每 5 秒取概览；页面进入后台后均降为 30 秒。前端保留最近 24 个样本，并把网络累计字节按真实采样时间差转换为 B/s。

## 页面入口

桌面系统应用新增「Docker」。首屏的「Compose 管理」按钮直接打开 Issue #2 的「Compose 应用」窗口，不复制镜像、网络、卷或容器深层管理页面。
