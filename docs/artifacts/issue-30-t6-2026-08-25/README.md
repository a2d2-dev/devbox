# issue #30 T6 — 主题壁纸 tab 验收证据

## 已知集成问题(CEO 确认,后端另修)
本机 8080 部署无 users.db,`admin` 走 legacy 单密码登录,`principal.UserID` 为空。
后端 `/api/v1/account/preferences` 对空 UserID 返回 **401**
(`pkg/console/handlers_account_prefs.go` 的 `currentUserID`)。前端 `useApi.js`
`authFetch` 的全站约定是「401 + 有 token = 会话过期 → clearAuth 登出」,因此 legacy admin
下 useTweaks 的 hydrate GET / 防抖 PUT 一发出即触发全局登出(表现为「突然回登录页」)。

- **定性**:后端 401 语义 bug,CEO 已确认并另派 hotfix(空 UserID 改 403 +
  `reason=not_a_managed_account`)。本前端票**不修改** `authFetch` 的 401 登出约定
  (全站行为,超范围)。
- **前端保证**:useTweaks 对任何非 200(含未来 403)一律静默降级 localStorage,
  不弹错、不重试风暴(hydrate `!resp.ok` 直接 return;PUT `.catch()` 吞错,单 timer 无循环)。
- legacy admin 无偏好存储、行为 = localStorage 降级,是**预期**,非 bug。

## 验收账号:store-backed 用户(有 UserID,链路完整 200)
用 legacy admin token 经既有 admin API `POST /api/v1/users` 创建测试用户:
- `uitest` / role=user / `docs/artifacts/.../uitest/`(CEO 指定,本轮主验收)
- `t6tester` / role=admin /(上一轮,同结论,保留作交叉证据)

curl 实测(uitest):GET 空账号返 `{}`[200];PUT 回显完整 8 键白名单[200];
证明端点对 store-backed 用户完全可用。

## 截图对照(uitest/ 子目录)
1. `01-appearance-tab-overview` — 主题壁纸 tab 全貌(默认 fnos/蓝/浅色)
2. `02a-wallpaper-topo-selected` + `02-desktop-topo-background` — 切光晕→桌面即时变 + 后端 PUT 持久化
3. `03a-after-reload-desktop-topo` + `03-after-reload-topo-purple-persisted` — 刷新后 topo+紫保持
4. `04a-fresh-login-desktop-restored` + `04-fresh-login-prefs-restored-from-backend`
   — **清空 localStorage 全新上下文重登 → topo+紫 纯后端恢复**(issue #30 硬验收)
5. `05-accent-purple-selected` — accent 紫 生效 + 保存

顶层目录另有 `t6tester` 前缀的等价截图(grid/绿),同结论。

## 说明
- 截图由 agent-browser 采集,内容以 a11y snapshot 的 `checked=true` + 后端 GET 双向佐证;
  像素级视觉观感未由本 agent 目视核对(模型不支持图像输入)。
- 测试用户 `uitest`/`t6tester` 遗留在 8080 后端 DB(验收产物,未改 config.yaml);
  如需清理调 users API 删除。
