# 下载任务中心

DevBox 下载任务中心提供本机 HTTP(S) 直链下载、暂停恢复、任务持久化和桌面管理界面。当前不依赖 aria2 等外部进程。

## 数据与目录

- 下载根目录与文件浏览器共用 `console.work_dir`，未配置时为 `/data`。
- API 的 `targetDirectory` 可使用根目录内的相对路径，也可使用根目录内的绝对路径；空值默认为 `downloads`。
- 包含 `..` 的路径、根目录外路径和通过符号链接逃逸根目录的路径均会被拒绝。
- 任务状态保存在 `<console.work_dir>/.devbox/downloads.json`。状态文件使用临时文件加原子重命名更新。
- 未完成内容保存在目标文件旁的 `<文件名>.part`。完成后原子重命名为目标文件名。
- 下载文件的打开、最终重命名和删除都相对启动时持有的下载根目录句柄执行；父目录后来被替换为符号链接时不会逃逸根目录，`.part` 最后一级符号链接也不会被跟随。
- 进程重启时，`waiting` 和 `downloading` 任务恢复为 `paused`，保留已下载字节；用户需要手动开始。

DevBox 进程必须对下载根目录、目标目录和 `.devbox` 状态目录具有读写权限。初始化失败时列表 API 返回 `503`；目标目录无权限时创建任务返回 `403`。

下载默认拒绝解析到回环、RFC1918、链路本地、组播、未指定地址和 IPv6 ULA 的目标，并在每次重定向后重新执行限制；HTTPS 任务不能降级重定向到 HTTP。可信内网场景可显式设置 `console.allow_private_networks: true` 放开地址限制，协议降级仍保持禁用。

## 状态机

| 状态 | 含义 | 可执行操作 |
| --- | --- | --- |
| `waiting` | 等待并发槽位或创建后未开始 | 开始、暂停、删除 |
| `downloading` | 正在接收数据 | 暂停、删除 |
| `paused` | 用户暂停或进程重启后恢复 | 开始、删除 |
| `completed` | 文件已完整写入目标路径 | 删除 |
| `error` | 网络、远端响应或本地写入失败 | 开始（重试）、删除 |

下载引擎默认同时运行 3 个任务。协议通过 `downloads.Protocol` 扩展，本版本只注册 `http` 和 `https`。

## REST API

所有接口位于 `/api/v1`，并受控制台现有认证中间件保护。错误响应格式为：

```json
{"error":"错误原因"}
```

### 创建任务

`POST /api/v1/downloads`

```json
{
  "url": "https://example.com/archive.tar.gz",
  "targetDirectory": "downloads/images",
  "start": true
}
```

`url` 只允许不含内嵌用户名/密码的 HTTP(S) URL。`start` 可省略，默认 `true`；设为 `false` 时任务保持 `waiting`。文件名取 URL 路径的最后一段。如果目标文件已经存在，或另一任务占用同一目标路径，返回 `409`，不会覆盖文件。

### 列表与状态计数

`GET /api/v1/downloads`

响应在同一次引擎快照中返回：

- `tasks`：任务列表，按创建时间倒序。
- `counts`：`all`、`waiting`、`downloading`、`completed`、`paused`、`error` 计数。
- `statistics`：实时下载/上传速率与累计下载/上传流量。HTTP 下载的上传速率和上传流量为 0。
- `rootDirectory`：服务端实际下载根目录。

### 任务详情

`GET /api/v1/downloads/{id}`

任务包含 `downloadedBytes`、`totalBytes`、`speedBytesPerSec`、`estimatedSeconds`、`resumeSupported`、`error`、目标路径和时间字段。远端未提供内容长度时，`totalBytes` 为 0，剩余时间不可计算。

### 开始或重试

`POST /api/v1/downloads/{id}/start`

适用于 `waiting`、`paused` 和 `error`。仅当 `.part` 存在且已记录 ETag 或 Last-Modified 时发送 `Range: bytes=<size>-` 与 `If-Range`；没有资源 validator 时从头下载。

远端返回 `206 Partial Content` 时必须同时匹配保存的 validator 和 Content-Range 起点才会追加写入。validator 变化或不安全的 `416` 响应会丢弃旧进度并从头下载；远端忽略 Range 并返回 `200` 时也会从头覆盖 `.part`。

### 暂停

`POST /api/v1/downloads/{id}/pause`

适用于 `waiting` 和 `downloading`。已接收数据和最新进度会持久化，临时文件保留。

暂停会取消当前 worker；再次开始前，引擎会等待旧 worker 完整退出，避免两个 generation 同时写入 `.part`。关键状态持久化失败时操作返回错误，任务转为 `error` 并保留原因。

### 删除

- `DELETE /api/v1/downloads/{id}?deleteFile=false`：仅删除任务记录，保留目标文件和 `.part`。
- `DELETE /api/v1/downloads/{id}?deleteFile=true`：删除任务记录，同时删除目标文件和 `.part`。

选择同时删除文件时，引擎先删除文件，再删除并持久化任务记录；文件删除失败时任务保留为 `error`，可修复权限或目录问题后重试。

桌面应用在发出删除请求前提供二次确认和明确的删除模式选择。批量开始、暂停和删除只遍历当前勾选的任务，并显示成功/失败数量。

## 限制

- 仅实现 HTTP(S) 直链，不支持 BT、磁力链接、做种和 aria2。
- 不进行分片并行下载，单任务使用一个 HTTP 请求流。
- 不覆盖已有目标文件，也不自动生成重名后缀。
- URL 文件名来自路径，不使用远端 `Content-Disposition` 改名。
- 本版本没有上传协议，因此全局上传速率与累计上传流量为 0；字段保留供未来协议扩展。
