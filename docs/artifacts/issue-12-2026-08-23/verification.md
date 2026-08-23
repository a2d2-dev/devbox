# Issue #12 合并 origin/main 回归验证

## 实验目的

验证 `feat/issue-12-users-storage` 合并 `origin/main` 后仍保持 fail-closed 多用户认证、普通用户文件根授权与管理员角色边界；确认新合入的下载、备份、Docker、日志、维护和共享配置写端点拒绝普通用户；确认快捷键不会再因 `minimizeWindow` 未定义导致空白页。

## 实验步骤

1. 执行 `make ui`，从合并后的 `console-ui` 重建并同步 `pkg/console/dist`。
2. 在 `/data` 下创建独立 `TMPDIR`，执行 `go build ./...`、`go test -race ./pkg/auth ./pkg/users ./pkg/console` 和 `npx vitest run`。
3. 构建 `bin/devbox-issue12`，通过 supervisor 重启隔离服务 `devbox-issue12`，监听 `0.0.0.0:9097`。
4. 将新 UI 同步到该服务配置的 `/dev/shm/issue-12-ui`，确认实际 HTML 引用 `index-W6IakeY8.js`。
5. 用 agent-browser 管理员会话打开 `http://10.126.126.12:9097` 并登录，检查桌面、浏览器 errors/console，实际触发 `Ctrl+Alt+M`。
6. 在管理员 UI 创建临时普通用户 `mergeqa`，用独立 agent-browser 会话通过登录页进入桌面。
7. 在普通用户浏览器上下文请求新管理端点，记录 HTTP 状态；再打开“用户与权限”页面核对可见的权限提示与网络状态。

## 实验记录

### 自动化验证

| 命令 | 结果 | 输出摘要 |
| --- | --- | --- |
| `make ui` | 通过 | Vite 482 modules，生成 `index-W6IakeY8.js`，同步到 `pkg/console/dist` |
| `go build ./...` | 通过 | 退出码 0，无编译错误 |
| `go test -race ./pkg/auth ./pkg/users ./pkg/console` | 通过 | auth 5.885s；users cached；console 187.606s |
| `npx vitest run` | 通过 | 10 个测试文件、48 个测试全部通过，3.02s |
| `git diff --cached --check` | 通过 | 无冲突标记或空白错误 |

新增的 `TestRegularUserCannotCallPrivilegedWriteEndpoints` 以真实普通用户 Principal 覆盖 24 个特权请求；多用户文件测试继续覆盖授权目录列表、越界内容访问与符号链接逃逸。

### 浏览器行为

- 管理员通过凭据 vault 登录成功，桌面完整渲染，新模块入口与“用户与权限”同时可见。
- 新 bundle 下 agent-browser `errors` 与 `console` 均为空。实际触发 `Ctrl+Alt+M` 后仍为空，没有 `minimizeWindow is not defined`。
- 初次打开曾加载外置静态目录遗留的 `index-BcJXJCk2.js` 并复现旧 ReferenceError；同步本次构建并确认服务改为加载 `index-W6IakeY8.js` 后问题消失。该旧 bundle 结果不计为本次通过证据。
- 普通用户 `mergeqa` 通过真实登录页进入桌面；打开用户管理后页面显示“当前账户不是管理员，无法访问用户管理”，网络记录为 `GET /api/v1/users` 返回 `403`。

### 普通用户新端点边界

下列请求均返回 `403`，响应为 `{"error":"forbidden","message":"需要管理员权限"}`：

| 能力 | 请求 |
| --- | --- |
| 下载删除文件 | `DELETE /api/v1/downloads/{id}?deleteFile=true` |
| 备份任务写 | `POST /api/v1/backups` |
| Docker 服务控制 | `POST /api/v1/docker/service` |
| Docker 数据迁移 | `POST /api/v1/docker/storage/execute` |
| 日志清空 | `DELETE /api/v1/audit/events` |
| 维护重置 | `POST /api/v1/maintenance/reset` |
| WebDAV 设置写 | `PUT /api/v1/maintenance/settings` |
| SMB 配置应用 | `POST /api/v1/maintenance/smb/apply` |
| 进程终止 | `POST /api/v1/processes/{pid}/terminate` |
| onboarding 写 | `PATCH /api/v1/onboarding` |
| 文件删除 | `POST /api/v1/files/delete` |

## 截图证据

- [合并后管理员桌面](merge-admin-desktop.png)
- [快捷键触发后桌面](merge-minimize-shortcut.png)
- [普通用户桌面](merge-normal-user-desktop.png)
- [普通用户 403 页面](merge-normal-user-403.png)
