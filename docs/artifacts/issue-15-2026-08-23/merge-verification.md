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
