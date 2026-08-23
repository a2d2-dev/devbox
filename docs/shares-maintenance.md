# 文件访问服务与系统维护

## 产品定位

DevBox 是桌面算力平台，不是 NAS。本功能参考 fnOS 的配置体验，但只保留开发工作流需要的文件访问和主机内 DevBox 维护能力。

保留：

- WebDAV：DevBox 内置 Go 服务，适合编辑器、脚本和跨平台文件访问。
- SMB：探测系统已有 Samba，仅管理 DevBox 生成的共享配置片段。
- SMTP 通知、DevBox 版本检查、配置备份/还原、DevBox 数据重置、默认应用和关于页。

不做：

- DLNA：媒体库发现与转码不属于桌面算力平台。
- Time Machine：备份目标、配额和 Apple 兼容矩阵属于 NAS 能力。
- FTP/FTPS：WebDAV 已覆盖开发者远程文件访问，避免增加明文协议和证书维护面。
- NFS：需要主机导出、UID/GID 映射和网络信任策略，不作为桌面默认能力。
- UPS：硬件电源管理和关机编排不属于本票。
- OS 级更新、OS 级恢复出厂、自动安装 Samba：这些操作不可逆或改变宿主机软件供应链。
- fnOS 商业许可证：DevBox 使用 Apache License 2.0，不引入 fnOS 的商业授权模型。

## WebDAV

WebDAV 独立监听配置的 HTTP 端口，用户名固定为 `devbox`，密码复用当前控制台账户。控制台未配置密码时拒绝启用 WebDAV。

共享路径必须是 `console.work_dir` 指向数据根内已存在的目录。校验会同时解析数据根和目标目录的符号链接；`..` 穿越和指向数据根外的符号链接都会被拒绝。只读模式拒绝 `PUT`、创建目录、删除和重命名。

保存启用配置时先实际绑定端口。端口冲突会返回明确错误且配置不保存。服务在 DevBox 进程退出时优雅关闭。

验证示例：

```bash
curl -u devbox:'<console-password>' -X PROPFIND -H 'Depth: 1' http://10.126.126.12:19000/
curl -u devbox:'<console-password>' -T ./sample.txt http://10.126.126.12:19000/sample.txt
curl -u devbox:'<console-password>' http://10.126.126.12:19000/sample.txt
```

WebDAV 当前是 HTTP。需要跨不可信网络时，应在 DevBox 前放置受管 TLS 反向代理；本票不生成证书。

## SMB

DevBox 通过 PATH 查找 `smbd` 与 `testparm`，并用 `systemctl is-active smbd.service` 获取实际运行状态。未安装时只展示系统包安装指引，不执行 `apt install`。

共享模型包含共享名、数据根内路径、只读/读写和 guest。共享名只允许字母、数字、点、下划线和连字符，以阻止 smb.conf 段注入。预览不写文件。

应用顺序：

1. 在目标目录创建临时候选文件。
2. 对候选文件执行 `testparm -s`。
3. 若 smbd 正在运行，用 `smbstatus` 检查共享连接。无法检查或存在活动连接时拒绝写入与 reload。
4. 原子替换受管 include 文件，默认 `/etc/samba/devbox-shares.conf`。
5. 仅当 smbd 已运行且没有活动连接时执行 `systemctl reload smbd.service`，不 restart。

主 Samba 配置须由管理员预先包含受管文件：

```ini
[global]
    include = /etc/samba/devbox-shares.conf
```

可用 `DEVBOX_SMB_INCLUDE_PATH` 改变受管文件位置。DevBox 不修改发行版维护的 `/etc/samba/smb.conf`，也不管理 Samba 用户数据库。

## SMTP 通知

SMTP 支持明文、STARTTLS 和隐式 TLS，支持账号密码认证。密码只在进程内以明文参与 SMTP 握手；落盘使用随机 256 位本机密钥和 AES-GCM 加密，状态文件与密钥权限均为 `0600`。

“发送测试邮件”直接使用表单值连接服务器，不先把明文密码写入状态文件。日志只记录发送错误，不记录请求体、用户名或密码。本票将控制台登录失败接到通知钩子作为一个示例；SMTP 未启用时钩子为空操作。

## 更新与关于

版本检查使用 GitHub `releases/latest` API，仓库默认 `a2d2-dev/devbox`，可关闭检查。自动更新只保存开关，不下载或执行版本替换。

关于页展示构建时传入的 DevBox 版本、Apache License 2.0 和主要运行时依赖 attribution。DevBox 不采用 fnOS 商业许可证。

## 配置备份与还原

导出格式是 tar.gz，包含 `manifest.json` 和 `config.json`。默认脱敏导出不包含 SMTP 密码；只有显式选择“包含敏感信息”才把密码写入归档内权限为 `0600` 的配置项。归档应按密钥材料保护。

还原分两步：先上传并预览顶层配置差异，再输入 `RESTORE` 确认。确认后先在维护状态目录的 `backups/` 下创建包含密钥的当前配置备份，再原子保存候选配置并应用 WebDAV 状态。预览令牌十分钟失效；上传上限 8 MiB；归档路径穿越会被拒绝。

## DevBox 重置

重置要求勾选影响确认并输入 `RESET DEVBOX`。当前安全子集停止 WebDAV，清除本票维护的配置、SMTP 密钥、还原预览和自动备份，恢复默认值。它不重置操作系统、磁盘、系统服务、Samba 用户库或外部应用数据。

## 默认应用

最小文件关联模型按 MIME 类型保存桌面打开方式。目前页面提供纯文本、Markdown 和 JSON 的默认应用选择。配置作为 DevBox 维护设置参与导出和还原。

## 运行时路径与权限

| 项目 | 默认值 | 覆盖方式 |
|---|---|---|
| 数据根 | `/data` | `console.work_dir` |
| 维护状态 | `/var/lib/devbox/maintenance` | `DEVBOX_MAINTENANCE_DIR` |
| SMB 受管 include | `/etc/samba/devbox-shares.conf` | `DEVBOX_SMB_INCLUDE_PATH` |

生产服务账户需要维护状态目录的读写权限。执行 SMB apply 还需要受管 include 目录写权限及 `testparm`、`smbstatus`、`systemctl reload smbd.service` 权限。
