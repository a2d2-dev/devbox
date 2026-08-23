# Issue #18 对抗式 Review 整改验收记录

## 实验目的

验证进程终止 PID 身份保护、危险操作审计先行、异步任务终态审计、结构化日志脱敏、进程采样身份、可信审计身份与 session 清理均符合整改清单；同时确认合并 `origin/main` 后 Go/React 构建和既有测试未回归。

## 实验步骤

1. 分模块执行新增红灯测试，确认旧实现分别暴露审计写失败后仍发信号、PID 复用误用旧身份、typed payload/header 泄漏、I/O 解析错误伪装为可用、客户端 username/XFF 被信任、异步失败无终态审计等问题。
2. 每个模块修复后重复对应定向测试，直至转绿。
3. 执行 `go build ./...`。
4. 执行 `go test -race ./pkg/syslog ./pkg/console ./pkg/collector`。
5. 在 `console-ui` 执行 `npm test -- --run`，再执行 `make ui` 重建并同步 `pkg/console/dist`。
6. 通过 supervisor 临时托管构建后的 Vite preview，在 `http://10.126.126.12:4188` 使用 `agent-browser` 检查交互树、页面错误和截图；验证完成后关闭浏览器并移除临时服务。

## 实验记录

| 项目 | 结果 | 记录 |
| --- | --- | --- |
| Go build | 通过 | `go build ./...`，退出码 0 |
| Race tests | 通过 | syslog 36.917s；console 75.859s；collector 1.019s |
| Vitest | 通过 | 7 个文件、38 个断言全部通过 |
| Compose Node tests | 通过 | 7/7 通过 |
| UI build | 通过 | Vite 476 modules；生成 `index-jnGwtHsz.js`，`make ui` 已同步 dist |
| 浏览器页面 | 通过 | 登录页标题和账号/密码/保持登录/提交控件均存在；`agent-browser errors` 无输出 |
| 浏览器截图 | 通过 | [login-page.png](./login-page.png) |

补充诊断：额外执行的 `go test ./pkg/auth ./pkg/syslog ./pkg/apps ./pkg/console ./pkg/collector -count=1` 中，auth/syslog/console/collector 通过；`pkg/apps` 在 10 分钟超时，堆栈停在既有 `TestDiscoverUnregisteredDevboxProjectVisible` 创建 SQLite 仓库时的 `fsync`，未出现断言失败。本任务新增的 `TestWorkerNotifiesObserverOfFailedTerminalTask` 已单独通过（22.284s）。该额外全包命令不属于任务书指定验收命令。

已知非阻断警告：npm 读取仓库 `.npmrc` 时提示空 proxy URL；Vite 提示主 chunk 大于 500 kB。两项均为既有构建配置/体积提示，本票未调整。
