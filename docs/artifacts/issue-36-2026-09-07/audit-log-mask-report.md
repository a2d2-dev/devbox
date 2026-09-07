# Issue #36 AuditLog 脱敏验证记录

## 实验目的

验证审计日志历史页和查询响应只暴露脱敏后的来源 IP 与粗粒度设备信息，不暴露完整 IPv4、IPv6 或原始 User-Agent；同时确认审计写入/存储仍保留完整取证字段。

## 实验步骤

1. 运行后端针对性测试：
   `go test ./pkg/console -run 'TestAuditHandlerMasksIPAndUserAgentButKeepsStoreRaw|TestAuditHandlerFiltersPagesAndClearAuditsItself|TestAccountSessionsMasksIPAndUA|TestMaskIP|TestParseUA'`
2. 运行前端新增测试：
   `npx vitest run src/pages/AuditLog.test.jsx`
3. 运行全量后端测试：
   `go test ./...`
4. 运行前端完整测试：
   `npm test`
5. 启动 mock API 与 Vite dev server，使用 `agent-browser` 打开 `http://10.126.126.12:5174/`，进入日志应用并打开审计详情抽屉截图。

## 实验记录

- 后端针对性测试通过：`ok github.com/a2d2-dev/devbox/pkg/console 0.042s`
- 前端新增测试通过：`Test Files 1 passed (1)`, `Tests 2 passed (2)`
- 全量后端测试通过：`go test ./...` 所有包通过
- 前端完整测试通过：Vitest `14 passed (14)`, `60 passed (60)`；node:test compose 用例 `7 pass`
- 浏览器截图：`audit-log-masked-detail.png`
- 截图中详情抽屉显示 `源 IP 203.0.113.x` 与 `设备 Chrome · macOS (desktop)`，未显示原始 User-Agent。

## 备注

- `npm ci` 和前端测试期间出现既有 `.npmrc` 空 proxy warning。
- `npm ci` 报告 8 个依赖审计告警，本次未处理，属于本 issue 范围外。
