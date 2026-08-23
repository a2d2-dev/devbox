# 用户管理与硬件概览

## 产品定位

DevBox 是桌面算力平台，不是 NAS。本功能只提供控制台多用户账户、文件根目录授权、数据挂载点与硬盘的只读概览。不会迁移 fnOS 的存储空间创建、SSD 缓存、硬盘休眠、RAID、分区或格式化能力，也不会调用任何磁盘写命令。

## 账户与认证模型

- 控制台账户保存在 SQLite `users.db`。默认位于 `console.browser_data_path` 的同目录；示例配置对应 `/etc/devbox/users.db`。
- 密码只保存 bcrypt 哈希，不保存或返回明文。用户名不区分大小写且唯一。
- 角色为 `admin`（管理员）与 `user`（普通用户）。用户可被禁用，禁用、删除、改角色或重置密码会撤销该用户已有会话。
- 系统拒绝删除、禁用或降级最后一个已启用管理员。
- 首个数据库账户固定创建为已启用管理员；请求中的其他角色或禁用状态会被忽略，避免空库失去管理入口。
- `auth.password` 保留为兼容入口：数据库没有用户时保持原有单密码行为；数据库已有用户后仅用户名 `admin` 可使用该密码，避免普通用户误用旧密码取得管理员权限。
- 未配置旧密码且用户库为空时，控制台保持原有免认证模式，可创建首个管理员。首个账户创建后认证自动启用。
- 用户库路径一旦配置，打开失败或查询失败时认证 fail-closed：登录和受保护 API 返回服务不可用，不会降级为匿名访问。
- 登录失败按客户端 IP 与规范化用户名组合计数，1 分钟内连续失败 5 次后返回 `429`。不存在和禁用用户都会执行固定 dummy bcrypt 比较并返回相同登录失败结果。

用户名要求 3-32 位，以字母或数字开头，仅允许字母、数字、`.`、`_`、`-`。密码为 10-128 位，并且大写、小写、数字、符号四类中至少包含三类；创建或改密时拒绝首尾空白。用户资料与直接文件根授权由同一个 SQLite 事务提交，任一步失败都会整体回滚。

## 用户组与文件授权

文件根目录必须是 `console.work_dir` 内已经存在的绝对目录。普通用户的有效授权是直接授权与所在用户组授权的并集。管理员及兼容管理员可访问整个 `console.work_dir`。

文件 API 的列表、上传和内容预览均在服务端检查授权。工作区根、授权根和请求目标先通过 `EvalSymlinks` 解析为真实路径，再执行目录边界判断；指向工作区或授权根外的符号链接返回 `403`。工作区顶层列表仅返回通向已授权根目录的路径；前端过滤不是安全边界。

数据库关系如下：

```text
users --< group_members >-- user_groups
  |                              |
  +--< user_file_roots           +--< group_file_roots
                \                 /
                  >-- file_roots
```

## HTTP API

角色矩阵中的“登录用户”包含 `admin` 与 `user`；文件操作还必须通过该用户的目录授权。`-` 表示无需 bearer token。

| 端点（方法） | 匿名 | 普通用户 | 管理员 | 说明 |
| --- | --- | --- | --- | --- |
| 静态资源、`/metrics`、`/app-icons/*` | 允许 | 允许 | 允许 | 非 `/api/v1/` 资源与 Prometheus 抓取 |
| `/api/v1/auth/*`、`GET /health`、`GET /device`、`GET /cloud/status`、`GET /about` | 允许 | 允许 | 允许 | 登录、认证状态与登录页探测 |
| `GET /metrics*`、`GET /network*` | 拒绝 | 允许 | 允许 | 控制台实时监控 |
| `/network/remote-access`、`/network/ddns/*`、`/security/*` | 拒绝 | `403` | 允许 | 网络入口、SSH、防火墙、认证因子、封禁与证书设置，含只读状态 |
| `GET /processes*`、`GET /disks*`、`GET /gpu/processes`、`GET /ai/activity`、`GET /ai/transcript` | 拒绝 | 允许 | 允许 | 系统只读监控 |
| `POST /processes/{pid}/terminate` | 拒绝 | `403` | 允许 | 终止宿主进程 |
| `GET /hardware*`、`GET /ports`、`GET /models`、`GET /alerts` | 拒绝 | 允许 | 允许 | 硬件、端口、模型与告警只读数据 |
| `GET /store/*`、`GET /catalogs*` | 拒绝 | 允许 | 允许 | 商店与 catalog 浏览；不含安装和 source 写操作 |
| `POST /apps/validate`、`GET /apps*`、`GET /tasks/*` | 拒绝 | 允许 | 允许 | 应用校验、清单、详情、日志、任务与预览 |
| `POST /apps`、`PUT/DELETE /apps/{id}`、`POST /apps/{id}/*` | 拒绝 | `403` | 允许 | 应用创建、更新、接管、启停、恢复与卸载 |
| `POST /store/install`、`POST /catalogs/install`、catalog/source 非 GET | 拒绝 | `403` | 允许 | 应用安装、catalog 刷新与 source 配置 |
| `GET /supervisor/status`、`GET /supervisor/services/{name}/logs` | 拒绝 | 允许 | 允许 | Supervisor 只读状态和日志 |
| `POST /supervisor/services/{name}/control`、`/terminal/exec` | 拒绝 | `403` | 允许 | 宿主服务控制与宿主 shell |
| `GET /vms*` | 拒绝 | 允许 | 允许 | VM 清单与详情 |
| `POST /vms/{name}/control`、`POST /vms/{name}/config` | 拒绝 | `403` | 允许 | VM 启停与配置 |
| `GET /files`、`GET /files/search`、`GET /files/content`、`GET /files/download` | 拒绝 | 按授权目录 | 允许 | 普通用户仅能读取获授权文件根，且仅可见“我的文件”来源 |
| `POST /files/upload`、`POST /files/mkdir`、`POST /files/rename`、`POST /files/transfer` | 拒绝 | 按授权目录 | 允许 | 普通用户仅能在获授权文件根内写入或移动 |
| `/files/delete`、`/files/trash*`、`/files/favorites`、`/files/recent`、`/files/shares*` | 拒绝 | `403` | 允许 | 删除及全局回收站、收藏、最近、外链状态仅管理员可管理；公开外链下载除外 |
| `GET /downloads*` | 拒绝 | 允许 | 允许 | 下载任务只读状态 |
| `POST /downloads*`、`DELETE /downloads/{id}` | 拒绝 | `403` | 允许 | 新建、启停、删除任务及可选删除已下载文件 |
| `GET /backups*` | 拒绝 | 允许 | 允许 | 备份任务、历史、版本与日志只读数据 |
| `POST /backups*` | 拒绝 | `403` | 允许 | 备份预检、创建、运行、暂停与恢复 |
| `GET /docker/overview`、`GET /docker/stats` | 拒绝 | 允许 | 允许 | Docker 服务与容器资源只读概览 |
| `/docker/service`、`/docker/autostart`、`/docker/storage/*` | 拒绝 | `403` | 允许 | Docker 服务控制、开机启动与数据目录迁移 |
| `GET /maintenance/settings`、`GET /maintenance/updates/check`、`GET /maintenance/about` | 拒绝 | 允许 | 允许 | 脱敏设置、更新检查与版本信息 |
| `PUT /maintenance/settings`、`POST /maintenance/smb/*`、`POST /maintenance/smtp/test` | 拒绝 | `403` | 允许 | WebDAV/SMB/SMTP 等配置写与探测 |
| `GET /maintenance/backup`、`POST /maintenance/restore/*`、`POST /maintenance/reset` | 拒绝 | `403` | 允许 | 配置导出、还原与维护配置重置 |
| `GET /onboarding`、`PATCH /onboarding` | 拒绝 | 允许 / `403` | 允许 | 普通用户可读引导状态，只有管理员可更新 |
| `/browser/*` | 拒绝 | 允许 | 允许 | 浏览器代理、探测、书签与历史 |
| `GET /links`、`GET /audit/events` | 拒绝 | 允许 | 允许 | 导航与审计只读数据 |
| `POST /links/reload`、`POST /alerts/{id}/ack`、`POST /ai/codex/cleanup-stale`、audit 非 GET | 拒绝 | `403` | 允许 | 系统级写操作 |
| `/users*`、`/user-groups*`、`/file-roots*` | 拒绝 | `403` | 允许 | 账户、组与文件授权管理，含读接口 |

## 硬件与挂载概览

`GET /api/v1/hardware` 在现有硬件快照中增加：

- `storage[]`：物理盘路径、型号、容量、内置/外接分类、HDD/SSD/NVMe 介质、接口、SMART 状态、分区与挂载用途。
- `mounts[]`：真实文件系统挂载路径、容量、已用、可用、使用率、文件系统、来源及所属磁盘。

信息来自 `lsblk`、`findmnt` 与可选的 `smartctl`。没有安装 `smartctl` 时状态为 `unsupported / smartctl 未安装`；权限不足时为 `permission_required / 需要权限读取 SMART`，不会伪装成健康值或数值 0。现有 `/api/v1/disks` 与 `/api/v1/disks/io` 继续提供分区 df 使用率和实时 I/O。

## 明确不做

本功能没有 Linux 系统用户写操作、存储空间向导、RAID、分区、格式化、挂载/卸载、SSD 缓存或硬盘休眠。合入的 WebDAV/SMB 配置属于独立维护模块，并受管理员角色边界保护；本功能只负责账户角色与文件根授权。
