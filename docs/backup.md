# 备份

DevBox 备份应用使用系统 `rsync` 在本机目录、外接设备挂载点和 SSH
服务器之间复制目录。任务、调度状态和运行历史由 `pkg/backup` 管理，console
仅负责 HTTP 映射。

## 数据模型

任务包含以下配置：

- 源与目标：`local`、`mount` 或 `ssh`。SSH 端点保存 `user@host`、端口、
  目录和可选 identity file 路径，不保存私钥内容。
- 存储模式：每个任务使用 `<target>/<taskID>/` 独立命名空间。`versioned` 在
  该命名空间内为每次运行创建 UTC 时间目录；`mirror` 始终同步到该任务命名空间。
- 版本传输：版本模式可选全量或硬链接增量。增量对前一版本传入
  `--link-dest=../<version>`，未变化文件共享 inode。
- 同步删除：可选 `--delete`。启用后目标中源端不存在的文件会被删除。
- 排除规则：每行一个 rsync `--exclude` pattern。
- 保留策略：版本模式保留按名称排序的最近 N 个合法时间版本。非版本目录
  不参与清理。

版本传输先写入任务命名空间内的 `.devbox-incomplete-*` 目录，rsync 成功后才
rename 为正式时间版本。`--link-dest`、版本枚举和 retention 均不越出该任务命名空间。
失败运行会清理暂存目录，因此不会把不完整数据暴露为可恢复版本或后续增量基线。

本机 DevBox 二进制默认将状态保存到 `/var/lib/devbox/backup/state.json`，文件以
`0600` 原子替换写入。备份包的嵌入方可通过 `backup.NewManager(dataDir, N, logger)`
覆盖数据目录和并发上限；console 当前默认并发上限 N 为 2。进程重启会加载全部
任务并恢复调度；无法解析的状态文件会先改名为 `state.json.corrupt-<timestamp>`，记录
warning 后以空状态继续启动。上次进程遗留的 queued/running 记录会补写结束时间并
标记为 interrupted 失败，不会伪报成功。

## 调度

支持每天、每周和五字段 cron。cron 字段顺序为 minute、hour、day-of-month、
month、day-of-week，支持 `*`、数字、列表、范围和步长，例如
`*/15 9-17 * * 1-5`。day-of-month 与 day-of-week 同时受限时采用标准 cron 的
OR 语义。调度器在进程内运行，暂停任务会移除 nextRunAt，恢复时从当前时间计算
下一次运行。手动立即执行不改变下一次计划时间。

备份和恢复共享一个并发槽池，同一任务同时只允许一个运行；指向同一规范化目标根
（本地路径解析符号链接后比较）的不同任务也会串行。状态依次为 queued、running、
success 或 failed。每次历史记录包含开始/结束时间、版本、传输量、失败
阶段、错误和最多 1 MiB 的 rsync 输出。失败阶段包括 preflight、prepare、transfer、
finalize、retention、restore-preview 和 restore-transfer。

## 预检

创建任务和每次运行前都执行预检：

- 本地源必须为可读目录；本地或挂载目标必须存在且通过真实临时文件写入测试。
  `mount` 端点还必须出现在 `/proc/self/mountinfo`，防止设备未挂载时写入宿主目录。
- 本地目标在 `EvalSymlinks` 后必须是允许根的子目录。默认允许根为 `work_dir` 和
  `/data`，可通过 `console.backup_allowed_roots` 扩展；允许根自身、其祖先以及 `/`、
  `/etc`、`/usr`、`/var`（`work_dir` 位于其内时例外）、`/boot`、`/proc`、`/sys`、
  `/dev`、`/run` 等系统路径均拒绝。
- SSH 使用 batch mode 和 5 秒连接超时。远端目标通过 `mktemp` 创建文件并立即删除，
  验证真实写入能力；包含 SSH 端点时预检结果会警告“未能检查路径循环”。
- 本地目标解析符号链接后不得等于源、嵌套在源内或成为源的祖先。
- 本地容量通过 `statfs`，远端容量通过 `df -Pk`；源估算使用本地遍历或远端
  `du -sk`。估算命令失败或已知容量不足时都会阻断，不伪造容量数值。
- rsync 不支持一次任务的远程源到远程目标，因此该组合在配置校验中阻断。

SSH 认证使用系统现有配置或指定的 identity file。identity file 必须放在备份数据目录
的专用 `keys/` 子目录（默认 `/var/lib/devbox/backup/keys/`），任务配置填写该目录内的
绝对路径；目录外路径和经符号链接逃逸的文件会拒绝。应预先配置 known_hosts 和
无交互认证；任务中只记录私钥路径，API、历史和日志均不读取或返回私钥内容。

示例配置：

```yaml
console:
  work_dir: /data/projects
  backup_data_dir: /var/lib/devbox/backup
  backup_concurrency: 2
  backup_allowed_roots:
    - /mnt/backup-volumes
```

## 恢复

恢复可以写回原源目录，也可以指定另一个已存在且可写、符合相同允许根策略的本地
目录；原源目录是策略例外。版本模式从任务命名空间内的选定时间目录恢复；镜像模式
使用 `mirror`。恢复分两步：

1. preview 使用 `rsync --archive --checksum --dry-run --itemize-changes` 生成变更和
   已存在文件的冲突清单，并返回由任务、版本、目标和清单计算的确认 token。
2. confirm 必须提交 `confirm=true` 和 token。执行前重新 preview；清单变化会拒绝旧
   token，要求再次确认。正式恢复也使用 checksum，避免相同大小和 mtime 的内容
   差异被跳过。

恢复不会传入 `--delete`，因此不会删除恢复目标中仅目标存在的文件。

当前 console 密码为空时 API 匿名可达属于平台既有认证模型；fail-closed 与角色授权由
Issue #12 统一处理，本备份票不重复引入局部认证分支。

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
- 版本目录命名为 `<target>/<taskID>/YYYYMMDDTHHMMSSZ`；同一秒重复运行追加数字后缀。
- 远端冲突预览无法直接 stat 目标文件，rsync 报告的远端变更均按潜在冲突展示。
