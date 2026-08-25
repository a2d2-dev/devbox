# Issue #30「个人设置」线上实机验收走查

- **执行**: codex（只读验收，未改代码、未开分支）
- **目标环境**: http://127.0.0.1:8080（生产 bundle，与 http://10.126.126.12 同一服务；真实部署，非 dev server）
- **时间**: 2026-08-25
- **验收账号**:
  - `admin` — legacy 单密码账号（密码只读自 `/etc/devbox/config.yaml` auth 段，未修改）
  - `acceptance` — 临时 store-backed 用户（用 admin token `POST /api/v1/users` 建，role=user，验收后已 `DELETE` 还原）

## 总体结论

| 项 | 结论 |
|----|------|
| 1. admin(legacy) 路径 | **PASS** |
| 2. acceptance(store-backed) 路径 | **PASS** |
| 3. 桌面完整性 / 控制台无红错 | **PASS** |
| 4. 收尾：删除 acceptance、用户列表还原 | **PASS** |

浏览器 console 全程 **0 error / 0 message**（含 error 级）。admin 密码验收后确认仍为原值（`authenticated:true`），无副作用。

---

## 环境事实与偏差说明（不影响验收判定）

- 本部署 `users.db` **存在** `admin` 行（id `e6890f96-…`），与任务背景「admin 无 users.db 行」措辞不符。
- 但 **legacy 单密码登录签发的会话 principal `UserID` 为空**（`GET /api/v1/account` 返回 `"id":""`），因此后端仍按不受管账号处理：
  - `GET /api/v1/account/preferences` → **403** `reason=not_a_managed_account`
  - `PATCH /api/v1/account`（改名）→ **400** `reason=not_a_managed_account`
  - `POST /api/v1/account/password`（改密）→ **400** `reason=not_a_managed_account`
  - `GET /api/v1/account/sessions` → **200**（可看设备）
- 即 users.db 中的 `admin` 行对 legacy 会话 principal 无效，验收点（拒绝路径）与背景口径一致。此处仅作事实记录。

---

## 逐项明细

### 1. admin（legacy）路径 — PASS

- 登录成功 → 顶栏用户菜单出现「个人设置」。截图 `00-login-page.png`、`01-admin-desktop.png`、`02-admin-usermenu.png`（菜单显示「admin 已登录 / 个人设置 / 退出登录」）。
- **我的账号 tab**：正常显示账户资料（用户名 `admin`、角色「管理员」、显示名 `admin`）+ 修改密码表单，**未白屏、未被踢出**。`03-admin-tab-account.png`。
  - 改显示名尝试 → 服务端 **400 not_a_managed_account** 合理拒绝；UI 停留在设置面板，**仍登录**，无白屏。`04-admin-account-edit-rejected.png`。
- **主题壁纸 tab**：正常展示壁纸/主题色/主题模式选项。`05-admin-tab-wallpaper-before.png`。
  - **关键回归**：在壁纸 tab 切换壁纸（飞牛→网格）→ 壁纸本地生效（radio `网格` checked=true），**URL 仍为控制台桌面、三 tab 仍在、未被踢回登录页**（hotfix 403 已生效，偏好 PUT 被拒绝但不登出）。`06-admin-wallpaper-switched-grid.png`。切换后已还原为飞牛。
- **登录设备 tab**：列出登录历史，IP 打码（`127.0.0.x` / `10.126.126.x`），UA 归纳（`Chrome · Linux`、`curl · Unknown OS`、`Safari · iOS` 等），当前会话带「本机」徽标，含「退出其他全部设备」按钮与脱敏说明。`07-admin-tab-devices.png`。

### 2. acceptance（store-backed）路径 — PASS

- 登录成功 → 桌面正常。`08-acceptance-desktop.png`。
- **改显示名成功**：`Acceptance QA` → `验收员 Aicky`，UI 即时刷新，`GET /api/v1/users` 确认已持久化。`09-acceptance-tab-account.png`、`10-acceptance-displayname-changed.png`。
- **改壁纸 + accent**：壁纸设为 `网格`(grid)、主题色设为 `紫`(#8b5cf6)。`GET /api/v1/account/preferences` 返回 `wallpaper:grid, accent:#8b5cf6`。`11-acceptance-wallpaper-accent.png`。
  - 备注：任务示例壁纸「topo」在本 bundle 中不存在，选用等价的非默认壁纸「网格」验证。
- **刷新保持**：`F5`/重载后仍登录，localStorage `edgex-user-prefs` 仍为 `wallpaper:grid, accent:#8b5cf6`。`12-acceptance-after-refresh.png`。
- **后端持久化硬验收**：**清空 localStorage + sessionStorage 后重新登录** → 偏好从服务端恢复为 `wallpaper:grid, accent:#8b5cf6`（客户端缓存为空，值来自后端）。`13-acceptance-prefs-restored-after-relogin.png`。
- **登录设备 tab**：当前会话带「本机」徽标，IP 打码，含脱敏说明与「退出其他全部设备」。`14-acceptance-tab-devices.png`。
- **改密**：
  - 旧密错误（`WrongOldPass-999`）→ 内联报错「**当前密码错误。**」，**未被登出**（仍在设置面板）。`15-acceptance-pwd-wrong-old.png`。
  - 正确旧密（`Accept-QA-2026x`）改新密（`NewAccept-QA-2027z`）→ 成功提示「**密码已修改，其它设备已退出登录。**」，调用者本会话保留。`16-acceptance-pwd-changed-ok.png`。
- **新密码重登成功**：清 storage 后用新密码浏览器重登进入桌面；API 双向确认：新密 `authenticated:true`(200)、旧密 401。`17-acceptance-relogin-newpwd.png`。

### 3. 桌面完整性 — PASS

- fnOS 桌面渲染正常（顶栏 CPU/内存/时钟在位、图标节点齐全、用户所选壁纸正确应用）。`18-desktop-integrity-full.png`。
- 窗口正常：打开「仪表盘」窗口，页面内容渲染（含 CPU/内存/概览文本）。`19-desktop-window-open.png`。
- **浏览器 console 全程 0 error / 0 message**（无未捕获异常）。403/400/401 为网络响应而非 console 红错，属预期拒绝路径。

### 4. 收尾 — PASS

- `DELETE /api/v1/users/{acceptance-id}` → **204**。
- 用户列表还原为原始 3 个（`admin` / `t6tester` / `uitest`），**无 `acceptance`**；admin UI「用户与权限」视图确认。`20-userlist-restored-no-acceptance.png`。
- admin 密码确认未变（仍 `devbox`，`authenticated:true`）。

---

## 未验证 / 说明

- 「退出其他全部设备」按钮仅做展示与存在性确认，未实际点击执行（避免吊销其他会话，属破坏性操作，超出只读验收范围）。
- 深色 / 跟随系统主题模式标注「即将支持」，未在验收范围内。
- 任务背景所述「admin 无 users.db 行」与本部署实际（存在 admin 行但 principal UserID 为空）存在措辞偏差，已在上文记录；对验收判定（拒绝路径）无影响。

## 截图清单

| 文件 | 内容 |
|------|------|
| 00-login-page.png | 登录页 |
| 01-admin-desktop.png | admin 登录后桌面 |
| 02-admin-usermenu.png | admin 用户菜单（含「个人设置」） |
| 03-admin-tab-account.png | admin · 我的账号 tab |
| 04-admin-account-edit-rejected.png | admin 改名被 400 拒绝、未登出 |
| 05-admin-tab-wallpaper-before.png | admin · 主题壁纸 tab |
| 06-admin-wallpaper-switched-grid.png | admin 切壁纸后仍登录（关键回归） |
| 07-admin-tab-devices.png | admin · 登录设备 tab（本机徽标） |
| 08-acceptance-desktop.png | acceptance 登录后桌面 |
| 09-acceptance-tab-account.png | acceptance · 我的账号 tab |
| 10-acceptance-displayname-changed.png | 改显示名成功 |
| 11-acceptance-wallpaper-accent.png | 改壁纸(grid)+accent(紫) |
| 12-acceptance-after-refresh.png | 刷新后偏好保持 |
| 13-acceptance-prefs-restored-after-relogin.png | 清 storage 重登后从后端恢复 |
| 14-acceptance-tab-devices.png | acceptance · 登录设备（本机徽标） |
| 15-acceptance-pwd-wrong-old.png | 旧密错误内联报错、未登出 |
| 16-acceptance-pwd-changed-ok.png | 正确旧密改密成功 |
| 17-acceptance-relogin-newpwd.png | 新密码重登成功 |
| 18-desktop-integrity-full.png | 桌面完整性 |
| 19-desktop-window-open.png | 窗口打开正常 |
| 20-userlist-restored-no-acceptance.png | 删除 acceptance 后用户列表还原 |
