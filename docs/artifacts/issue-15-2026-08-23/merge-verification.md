# Issue #15 合并验证记录

## 实验目的

验证 `feat/issue-15-appcenter` 合并 `origin/main` 后，App Center、系统应用和多用户
管理员授权语义能够共存，前后端构建及指定测试范围无回归。

## 实验步骤

1. 执行 `git fetch origin main` 与 `git merge origin/main`。
2. 保留 store/catalog preflight 路由，并让它们继续经过全局登录保护；安装与 catalog
   来源写操作沿用 `main` 的管理员保护。
3. 执行 `make ui`，从合并后的前端源码重建 `pkg/console/dist`。
4. 分别在 `/data` 下用 `mktemp -d` 创建 `TMPDIR`，执行 Go 构建、Go 测试和 Vitest。

## 实验记录

- `make ui`：通过，生成 `index-DrivFnM6.js` 与 `index-D9bIzPZ8.css`。
- `go build ./...`：通过，退出码 0。
- `npx vitest run`：通过，11 个测试文件、54 个测试全部通过，耗时 3.74 秒。
- `go test ./pkg/apps ./pkg/console`：默认 10 分钟门限首次因 `/data` 上 SQLite
  `fsync` 延迟超时；堆栈停在 `modernc/sqlite`，不是断言失败。
- `TestWorkerQueueFullDoesNotDrop`：原 10 秒等待窗把 80 个持久化任务的磁盘吞吐当成
  队列正确性。等待窗调整为 2 分钟后，定向测试在相同 `/data` TMPDIR 下通过，耗时
  70.536 秒。
- 完整 Go 测试在相同 TMPDIR 约束下以 40 分钟测试门限重跑并通过：
  `pkg/apps` 1643.572 秒，`pkg/console` 177.405 秒。

## 结论

合并后的应用中心、系统应用和认证路由通过构建与指定测试。preflight 需要登录但不要求
管理员；store/catalog 安装、应用写操作和 catalog 来源写操作要求管理员。

## 合并 Issue #13 增量

### 实验目的

验证 `feat/issue-15-appcenter` 在 HEAD `e59b1c8` 上继续合并最新
`origin/main` (`cc48ec7`，Issue #13 网络与安全) 后，应用中心与网络安全功能、认证和
管理员授权语义继续共存。

### 实验步骤

1. 执行 `git fetch origin main && git merge origin/main`。
2. 审查自动合并后的 `server.go`、`App.jsx` 和授权测试；确认 store/catalog preflight
   仍经过全局登录保护，install 和写端点仍要求管理员，Issue #13 安全端点全部由
   `requireAdmin` 包裹。
3. 对生成产物冲突执行 `make ui`，从合并后的源码统一重建 `pkg/console/dist`。
4. 创建 `TMPDIR=/data/tmp.EptIvBZ4WA`，执行指定的 Go 构建、console 测试和 Vitest。
5. 用本 worktree 构建的 embedded bundle 启动隔离验证实例，通过 `agent-browser`
   登录并打开应用商店和网络与安全页面。

### 冲突记录

- `pkg/console/dist/assets/index-W6IakeY8.js`：两侧分别重命名为不同哈希文件，属于
  `rename/rename` 生成产物冲突。未选任一侧旧 bundle，使用 `make ui` 生成同时包含
  两侧源码的 `index-DzcPcK47.js`。
- `pkg/console/dist/index.html`：两侧引用不同哈希 JS，使用同一次 `make ui` 重建为
  `index-DzcPcK47.js` 引用。
- `pkg/console/server.go`、`console-ui/src/App.jsx` 和 Settings 相关源码没有直接冲突。
  自动合并结果保留了 App Center 的两个 preflight 路由和全部管理员保护，同时加入
  Issue #13 的网络安全路由、限流、安全存储与前端入口。

### 实验记录

- `make ui`：通过；Vite 8.0.14 转换 486 个模块，生成
  `index-DzcPcK47.js` 和 `index-D9bIzPZ8.css`。有单 chunk 超过 500 kB 的构建警告，
  无构建失败。
- `TMPDIR=/data/tmp.EptIvBZ4WA go build ./...`：通过，退出码 0。
- `TMPDIR=/data/tmp.EptIvBZ4WA go test ./pkg/console`：通过，耗时 188.787 秒。
- `TMPDIR=/data/tmp.EptIvBZ4WA npx vitest run`：通过，13 个测试文件、58 个测试全部
  通过，耗时 7.05 秒。
- `pkg/apps`：本轮按任务书跳过；上一轮相同分支已全量通过，记录耗时 1643.572 秒。
- `agent-browser`：应用商店和网络与安全页面均成功打开；网络页读取到物理接口与
  `tun0 10.126.126.12/24`，浏览器 console/error 输出为空。

### 浏览器证据

- [桌面同时显示应用商店与网络安全入口](merge-issue13-desktop.png)
- [应用商店页面](merge-issue13-app-center.png)
- [网络与安全页面](merge-issue13-network-security.png)

### 增量结论

Issue #13 网络/安全增量与 App Center 合并后可共同构建、测试和运行。preflight 登录
保护、install/应用/catalog 写端点管理员保护均保持，生成产物已由合并源码统一重建。
