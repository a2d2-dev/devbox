# 备份

DevBox 备份应用使用系统 `rsync` 在本机目录、外接设备挂载点和 SSH
服务器之间复制目录。任务、调度状态和运行历史由 `pkg/backup` 管理，console
仅负责 HTTP 映射。

## 数据模型

任务包含以下配置：

- 源与目标：`local`、`mount` 或 `ssh`。SSH 端点保存 `user@host`、端口、
  目录和可选 identity file 路径，不保存私钥内容。
- 存储模式：`versioned` 为每次运行创建 UTC 时间目录；`mirror` 始终同步到
  同一目录。
- 版本传输：版本模式可选全量或硬链接增量。增量对前一版本传入
  `--link-dest=../<version>`，未变化文件共享 inode。
- 同步删除：可选 `--delete`。启用后目标中源端不存在的文件会被删除。
- 排除规则：每行一个 rsync `--exclude` pattern。
- 保留策略：版本模式保留按名称排序的最近 N 个合法时间版本。非版本目录
  不参与清理。

版本传输先写入任务专属 `.devbox-incomplete-*` 目录，rsync 成功后才 rename 为正式
时间版本。失败运行会清理暂存目录，因此不会把不完整数据暴露为可恢复版本或后续
增量基线。

本机 DevBox 二进制默认将状态保存到 `/var/lib/devbox/backup/state.json`，文件以
`0600` 原子替换写入。备份包的嵌入方可通过 `backup.NewManager(dataDir, N, logger)`
覆盖数据目录和并发上限；console 当前默认并发上限 N 为 2。进程重启会加载全部
任务并恢复调度；上次进程遗留的 queued/running 记录会标记为 interrupted 失败，
不会伪报成功。

## 调度

支持每天、每周和五字段 cron。cron 字段顺序为 minute、hour、day-of-month、
month、day-of-week，支持 `*`、数字、列表、范围和步长，例如
`*/15 9-17 * * 1-5`。day-of-month 与 day-of-week 同时受限时采用标准 cron 的
OR 语义。调度器在进程内运行，暂停任务会移除 nextRunAt，恢复时从当前时间计算
下一次运行。手动立即执行不改变下一次计划时间。

备份和恢复共享一个并发槽池，同一任务同时只允许一个运行。状态依次为 queued、
running、success 或 failed。每次历史记录包含开始/结束时间、版本、传输量、失败
阶段、错误和最多 1 MiB 的 rsync 输出。失败阶段包括 preflight、prepare、transfer、
finalize、retention、restore-preview 和 restore-transfer。

## 预检

创建任务和每次运行前都执行预检：

- 本地源必须为可读目录；本地或挂载目标必须存在且通过真实临时文件写入测试。
  `mount` 端点还必须出现在 `/proc/self/mountinfo`，防止设备未挂载时写入宿主目录。
- SSH 使用 batch mode 和 5 秒连接超时，分别执行远端目录读/写权限测试。
- 本地目标解析符号链接后不得等于源或嵌套在源内。
- 本地容量通过 `statfs`，远端容量通过 `df -Pk`；源估算使用本地遍历或远端
  `du -sk`。估算命令失败或已知容量不足时都会阻断，不伪造容量数值。
- rsync 不支持一次任务的远程源到远程目标，因此该组合在配置校验中阻断。

SSH 认证使用系统现有配置或指定的 identity file。应预先配置 known_hosts 和无交互
认证；任务中只记录私钥路径，API、历史和日志均不读取或返回私钥内容。

## 恢复

恢复可以写回原源目录，也可以指定另一个已存在且可写的本地目录。版本模式从选定
时间目录恢复；镜像模式使用 `mirror`。恢复分两步：

1. preview 使用 `rsync --archive --checksum --dry-run --itemize-changes` 生成变更和
   已存在文件的冲突清单，并返回由任务、版本、目标和清单计算的确认 token。
2. confirm 必须提交 `confirm=true` 和 token。执行前重新 preview；清单变化会拒绝旧
   token，要求再次确认。正式恢复也使用 checksum，避免相同大小和 mtime 的内容
   差异被跳过。

恢复不会传入 `--delete`，因此不会删除恢复目标中仅目标存在的文件。

## HTTP API

- `GET/POST /api/v1/backups`
- `POST /api/v1/backups/preflight`
- `GET /api/v1/backups/{id}`
- `POST /api/v1/backups/{id}/run`
- `POST /api/v1/backups/{id}/pause`
- `GET /api/v1/backups/{id}/versions`
- `GET /api/v1/backups/{id}/history`
- `GET /api/v1/backups/{id}/history/{historyId}/log`
- `POST /api/v1/backups/{id}/restore/preview`
- `POST /api/v1/backups/{id}/restore`

## 限制

- 不支持 S3、WebDAV、加密备份、远程到远程传输或私钥托管。
- scheduler 是单进程调度器，不提供多实例选主。
- 版本目录命名为 `YYYYMMDDTHHMMSSZ`；同一秒重复运行追加数字后缀。
- 远端冲突预览无法直接 stat 目标文件，rsync 报告的远端变更均按潜在冲突展示。
