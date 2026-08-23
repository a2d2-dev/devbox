# Issue #16 对抗式 review 整改验证

## 实验目的

验证下载中心的目录句柄隔离、资源 identity 续传、SSRF/重定向限制、暂停恢复 worker 栅栏、持久化错误处理、失败删除保留任务，以及 waiting 状态 UI 筛选在合并最新 main 后均正常工作。

## 实验步骤

1. 运行 `go build ./...`。
2. 运行 `go test -race ./pkg/downloads ./pkg/console`，并用 `-count=1` 复跑以排除测试缓存。
3. 在 `console-ui` 运行 `npm test`。
4. 运行 `make ui`，同步生产 bundle 到 `pkg/console/dist`。
5. 重建并通过 supervisor 重启 `devbox-issue16`，使用 agent-browser 打开 `http://10.126.126.12:19116`。
6. 经 main 的登录流程进入下载应用，在桌面和 390x844 视口检查 waiting tab、工具栏和空状态，并检查浏览器错误。

## 实验记录

- `go build ./...`：通过，退出码 0。
- `go test -race ./pkg/downloads ./pkg/console`：通过；`-count=1` 非缓存复跑中 downloads 用时 50.278s、console 用时 48.719s。
- `npm test`：Vitest 7 个文件、39 个测试全部通过；Node 7 个测试全部通过。
- `make ui`：通过；Vite 变换 477 个模块，dist 已同步。
- agent-browser：下载页显示 `全部 / 等待 / 下载中 / 完成 / 暂停 / 错误` 六个筛选项；桌面与移动视口均无页面错误。
- 桌面证据：[downloads-waiting-tab.png](downloads-waiting-tab.png)
- 移动证据：[downloads-waiting-tab-mobile.png](downloads-waiting-tab-mobile.png)
