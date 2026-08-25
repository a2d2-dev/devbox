# issue #30 T7 —「个人设置 → 登录设备」验收截图

日期：2026-08-25
分支：`feat/issue-30-t7-devices-tab`
环境：worktree `console-ui` `npm run dev`（Vite，`DEVBOX_API_TARGET=http://localhost:8080`，
`/api` 代理到本机 8080 真实后端），admin 登录（`admin` / 配置文件 `auth.password`）。

为让列表有多条记录，用 `curl` 对 `8080` 的 `/api/v1/auth/verify` 以不同 User-Agent
（iPhone / iPad / Windows-Edge / Android-Chrome / curl / macOS-Chrome）多次登录，制造
desktop / mobile / tablet / unknown 各类型的登录历史。密码只从
`/etc/devbox/config.yaml` 的 `auth` 段只读读取，未修改该文件。

## 截图

### 01-device-list.png — 设备列表（多条，含「本机」徽标）
登录历史倒序展示。每行 = 设备类型图标 + `deviceLabel`（如「Chrome · macOS」）+
脱敏 IP（`127.0.0.x`）+ 格式化中文时间（`2026-08-25 02:24`）。最新一条 `current=true`
的行以蓝底高亮并挂蓝底「本机」徽标。顶部说明：「显示最近登录记录。IP 与设备信息已脱敏。」

### 02-confirm-dialog.png —「退出其他全部设备」二次确认弹层
点击右上角 danger 风格按钮后弹出卡片内确认层（`role="dialog"`）：
「退出其他全部设备？将吊销你在其他设备上的全部登录会话，那些设备需要重新登录。
当前设备（本机）不受影响。」提供「取消 / 确认退出」。

### 03-after-logout-others.png — 退出其他后列表刷新
点击「确认退出」→ 后端 `POST /api/v1/account/logout-others` 返回 204，前端刷新列表并
显示绿色成功提示「已退出其他全部设备。」。

**重要说明（预期行为）**：本 tab 展示的是**登录历史记录**，不是当前活跃会话列表。
「退出其他全部设备」吊销的是其他设备的**登录会话**（token），而历史审计记录会保留展示。
因此退出其他设备后，历史列表内容不减少反而可能因本次操作产生新的登录/刷新记录而增加，
这是**预期**结果，页面底部也有对应文字说明。

### 空态（未单独截图）
「暂无登录记录」空态分支已实现（`EmptyStateText`）。admin 账户天然有登录历史，
无法在不伪造数据的前提下触发空态，故未单独截图；加载态与错误态分支同样已实现。
