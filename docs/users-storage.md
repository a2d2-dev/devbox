# 用户管理与硬件概览

## 产品定位

DevBox 是桌面算力平台，不是 NAS。本功能只提供控制台多用户账户、文件根目录授权、数据挂载点与硬盘的只读概览。不会迁移 fnOS 的存储空间创建、SSD 缓存、硬盘休眠、RAID、分区或格式化能力，也不会调用任何磁盘写命令。

## 账户与认证模型

- 控制台账户保存在 SQLite `users.db`。默认位于 `console.browser_data_path` 的同目录；示例配置对应 `/etc/devbox/users.db`。
- 密码只保存 bcrypt 哈希，不保存或返回明文。用户名不区分大小写且唯一。
- 角色为 `admin`（管理员）与 `user`（普通用户）。用户可被禁用，禁用、删除、改角色或重置密码会撤销该用户已有会话。
- 系统拒绝删除、禁用或降级最后一个已启用管理员。
- `auth.password` 保留为兼容入口：数据库没有用户时保持原有单密码行为；数据库已有用户后仅用户名 `admin` 可使用该密码，避免普通用户误用旧密码取得管理员权限。
- 未配置旧密码且用户库为空时，控制台保持原有免认证模式，可创建首个管理员。首个账户创建后认证自动启用。

用户名要求 3-32 位，以字母或数字开头，仅允许字母、数字、`.`、`_`、`-`。密码为 10-128 位，并且大写、小写、数字、符号四类中至少包含三类。

## 用户组与文件授权

文件根目录必须是 `console.work_dir` 内已经存在的绝对目录。普通用户的有效授权是直接授权与所在用户组授权的并集。管理员及兼容管理员可访问整个 `console.work_dir`。

文件 API 的列表、上传和内容预览均在服务端检查授权。工作区顶层列表仅返回通向已授权根目录的路径；直接请求未授权目录返回 `403`，前端过滤不是安全边界。

数据库关系如下：

```text
users --< group_members >-- user_groups
  |                              |
  +--< user_file_roots           +--< group_file_roots
                \                 /
                  >-- file_roots
```

## HTTP API

除登录与状态接口外，以下管理接口均要求管理员 bearer token。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/v1/auth/verify` | 用户名密码登录；兼容旧 password-only 请求 |
| `GET` | `/api/v1/auth/status` | 返回认证状态与当前账户 |
| `GET, POST` | `/api/v1/users` | 搜索/列出、新增用户 |
| `PUT, DELETE` | `/api/v1/users/{id}` | 编辑、重置密码、角色/状态、删除 |
| `GET, PUT` | `/api/v1/users/{id}/access-roots` | 用户直接文件根授权 |
| `GET, POST` | `/api/v1/user-groups` | 搜索/列出、新增用户组 |
| `PUT, DELETE` | `/api/v1/user-groups/{id}` | 编辑成员与授权、删除用户组 |
| `GET, PUT` | `/api/v1/user-groups/{id}/access-roots` | 用户组文件根授权 |
| `GET, POST` | `/api/v1/file-roots` | 列出、新增文件根目录 |
| `DELETE` | `/api/v1/file-roots/{id}` | 删除文件根目录及关联授权 |

## 硬件与挂载概览

`GET /api/v1/hardware` 在现有硬件快照中增加：

- `storage[]`：物理盘路径、型号、容量、内置/外接分类、HDD/SSD/NVMe 介质、接口、SMART 状态、分区与挂载用途。
- `mounts[]`：真实文件系统挂载路径、容量、已用、可用、使用率、文件系统、来源及所属磁盘。

信息来自 `lsblk`、`findmnt` 与可选的 `smartctl`。没有安装 `smartctl` 时状态为 `unsupported / smartctl 未安装`；权限不足时为 `permission_required / 需要权限读取 SMART`，不会伪装成健康值或数值 0。现有 `/api/v1/disks` 与 `/api/v1/disks/io` 继续提供分区 df 使用率和实时 I/O。

## 明确不做

本功能没有 Linux 系统用户写操作、存储空间向导、RAID、分区、格式化、挂载/卸载、SSD 缓存或硬盘休眠，也不包含 SMB 等共享协议和网络安全配置。这些能力不符合桌面算力平台本票的只读硬件检查定位。
