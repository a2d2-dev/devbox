# 文件管理

DevBox 文件管理采用 fnOS 的“位置 + 集合”信息架构，但按单机开发环境收敛了来源和分享模型。所有来源使用相同的 `source + 相对 path` 列表模型，前端根据服务端返回的 capabilities 启用操作。

## 来源映射

| fnOS 概念 | DevBox 映射 | 写入 | 回收站 |
|---|---|---:|---:|
| 我的文件 | `console.work_dir`，默认 `/data` | 是 | 是 |
| 团队文件 | 不保留；DevBox 无多用户/团队空间 | - | - |
| 外接存储 | 自动发现 `/proc/mounts` 中 `/media`、`/mnt`、`/run/media`、`/Volumes` 下的挂载点 | 否 | 否 |
| 远程挂载 | 自动发现 NFS、CIFS/SMB、SSHFS、Ceph、9P 等已挂载文件系统 | 否 | 否 |
| 应用文件 | `compose.data_dir/apps`，默认 `/var/lib/devbox/apps` | 是 | 否 |

外接和远程来源只负责查看与下载，不提供修改操作。应用文件允许修改，但不使用主数据根的回收站；删除会明确提示并要求二次确认后永久执行。

即使挂载点位于主根或应用根内部，也只通过其独立只读来源访问；父来源会隐藏该目录并拒绝手工路径注入，避免挂载点错误继承写入和回收站能力。

应用目录任意层级的 `.env` 可能包含 Compose secret，服务端不会列出、搜索、下载或分享这些文件，直接构造路径也返回拒绝。

“他人共享 / 我的共享”依赖用户体系，DevBox 当前是单用户环境，因此合并为“外链管理”：创建只读下载 token、选择有效期、查看和撤销。

应用文件对应 `pkg/apps.Paths.AppDir` 的父目录；服务启动时从 `compose.data_dir` 推导，因此自定义 Compose 数据根会同步反映到文件来源。

## 文件能力

- 当前目录递归搜索最多 8 层、200 条结果；遇到 symlink 不跟随。
- 列表支持名称、大小、修改时间升降序；目录始终排在文件之前。
- 支持后退、前进、刷新、多选、上传、下载、新建文件夹、重命名、同来源移动/复制和删除。
- 收藏和最近访问由服务端持久化。最近访问在文件内容或下载成功解析时记录，最多保留 100 条。
- 图片仍通过认证请求读取 Blob 预览；剪贴板图片可直接上传到支持写入的来源。

上传沿用现有 20 MiB 单请求限制。同来源复制最多处理 10,000 个条目，超过限制会中止。

## 回收站

主数据根的布局：

```text
<work_dir>/.trash/
  index.json
  files/<random-id>
<work_dir>/.devbox-files/
  state.json
  audit.jsonl
```

普通删除先以随机 ID 将内容原子移动到 `.trash/files/`，`index.json` 记录来源、原相对路径和删除时间。恢复时重新校验原路径；父目录不存在会安全重建，原路径已被占用则返回冲突，不覆盖新内容。

永久清理先把索引项标记为 `pendingPurge` 并持久化，再删除实际内容；pending 项不再出现在回收站列表中。删除失败时保留 pending 状态，使用同一 ID 重试即可继续清理，避免内容已删除但索引仍显示的幽灵条目。清空回收站使用相同的两阶段顺序。

永久删除、回收站单项永久删除、清空回收站均要求 HTTP 请求显式携带 `confirm: true`，前端也会二次确认。永久删除在执行前记录审计意图；危险操作结束后追加 `success` 或 `failure` 结果到 `.devbox-files/audit.jsonl`。

## 外链安全

创建外链时生成 256-bit 随机 token。响应只在创建时返回明文 token；持久化文件仅保存 SHA-256 哈希。公开下载入口为：

```text
GET /api/v1/files/public/{token}
```

该入口是唯一免登录的文件 API。服务端每次下载都重新按 `source + path` 解析文件，不持久化或信任绝对路径。过期 token 返回 `403`，未知或撤销 token 返回 `404`。外链只支持普通文件，不支持目录。

## 路径安全模型

客户端只能提交来源 ID 和相对路径。服务端统一执行以下校验：

1. 拒绝绝对路径、卷名、NUL、反斜杠和规范化后含 `..` 的路径。
2. 来源 ID 必须来自当前配置或 `/proc/mounts` 发现结果。
3. 逐段 `Lstat` 拒绝任意 symlink，包括仍位于根内的 symlink；创建目标只允许末段不存在。
4. 实际读取、写入、递归遍历与删除使用 Go `os.Root` 的目录 fd 语义；即使校验后父目录被替换为 symlink，也不能越出来源根。
5. 重命名、移动与恢复在 Linux 上使用 `renameat2(RENAME_NOREPLACE)`，目标并发出现时返回冲突而不覆盖。文件系统不支持该操作时，普通文件回退为 `linkat + unlinkat`；目录明确返回不支持。
6. 下载和公开外链从已校验并打开的文件 fd 通过 `http.ServeContent` 输出，不按路径二次打开。
7. 外接/网络挂载点及其祖先目录不能从父来源执行递归复制或删除。

此策略有意不透明跟随 symlink，让读取和修改操作共享同一条可审计规则。

## HTTP API

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/v1/files/sources` | 来源及 capabilities |
| GET | `/api/v1/files` | 目录列表 |
| GET | `/api/v1/files/search` | 有界递归搜索 |
| POST | `/api/v1/files/upload` | multipart 上传 |
| GET | `/api/v1/files/content` | 认证内容/预览 |
| GET | `/api/v1/files/download` | 认证附件下载 |
| POST | `/api/v1/files/mkdir` | 新建文件夹 |
| POST | `/api/v1/files/rename` | 重命名 |
| POST | `/api/v1/files/transfer` | 同来源移动/复制 |
| POST | `/api/v1/files/delete` | 回收站或永久删除 |
| GET/POST | `/api/v1/files/trash*` | 列表、恢复、永久删除、清空 |
| GET/POST | `/api/v1/files/favorites` | 收藏列表/设置 |
| GET | `/api/v1/files/recent` | 最近访问 |
| GET/POST/DELETE | `/api/v1/files/shares*` | 外链创建、列表、撤销 |

旧调用仍可省略 `source`，默认使用 `my`；旧的 `content?path=<dir>&name=<file>` 形式也保留。
