# T07 reduced-motion 全量审计

审计时间：2026-07-17
审计命令：`rg -n "animation|transition|whileHover|whileTap|springs\.|animate" console-ui/src`

## 结论

- 审计范围内命中 43 个动效点，按组件/样式归并为 24 项。
- 发现 1 类缺失：多处内联 CSS `transition` 依赖全局兜底，但原 `@media (prefers-reduced-motion: reduce)` 未统一压缩 `transition-duration`。
- 已补齐：`console-ui/src/styles.css` 在 reduced-motion media 块中为 `*` / pseudo element 增加 `transition-duration: 0.001ms !important` 和 `transition-delay: 0ms !important`。
- `prefers-reduced-transparency` 代码层已覆盖材质类，但本轮 agent-browser/Chromium 未能模拟该媒体特性，运行验证记为未验证。

## 审计清单

| 动效点 | 降级方式 | 状态 |
|---|---|---|
| `styles.css` `.edge-pulse` 错误脉冲 | reduced-motion media 将 animation duration 压到 `0.001ms`、iteration 1 | 已覆盖 |
| `styles.css` `.edge-fade-in` / `.edge-backdrop-in` | reduced-motion media 将 animation duration 压到 `0.001ms` | 已覆盖 |
| `styles.css` `.edge-live-dot` / `.edge-cursor` 循环动画 | reduced-motion media 将 animation duration 压到 `0.001ms`、iteration 1 | 已覆盖 |
| `styles.css` `.edge-icon-hover` / `.edge-dock-item` hover 位移 | media 块禁用 hover transform，并压缩 transition | 已覆盖 |
| `styles.css` `.edge-press` 按压缩放 | media 块禁用 active transform，并压缩 transition | 已覆盖 |
| `styles.css` `.edge-menu-item` / 按钮 hover transition | 本次补丁统一压缩所有 transition duration/delay | 已补齐 |
| `styles.css` 材质类 `edge-material-*` / `.edge-glass` | `@media (prefers-reduced-transparency: reduce)` 改实底白色并关闭 blur | 代码已覆盖，运行未验证 |
| `motion.js` `PressScale` | `pref.reduced` 时不传 `whileTap`，transition 走 0.2s fade | 已覆盖 |
| `motion.js` `PopScale` | `pref.reduced` 时只做 opacity，不做 scale | 已覆盖 |
| `motion.js` `Fade` | 始终只做 opacity，使用 `fadeTransition` | 已覆盖 |
| `AppWindow.jsx` 打开/关闭/最小化/最大化 | `useMotionPref`; reduced 时去掉 x/y/scale，top/left/right/bottom/radius/shadow duration 0，仅 opacity 0.2s | 已覆盖 |
| `Dock.jsx` tooltip 进出场 | `pref.reduced` 时只做 opacity，不做 y/scale | 已覆盖 |
| `Dock.jsx` 图标 `whileHover` / `whileTap` | `pref.reduced` 时 `dockMotion = {}`，`pref.spring` 退化为 0.2s | 已覆盖 |
| `Toast.jsx` 进出场/堆叠 | `pref.reduced` 时只做 opacity，不做 y/scale；transition 0.2s | 已覆盖 |
| `TabBar.jsx` 指示条 layout 动画 | `pref.reduced ? { duration: 0 } : springs.snappy` | 已覆盖 |
| `ui.jsx` Ring 数值动画 | `pref.reduced ? { duration: 0 } : springs.default` | 已覆盖 |
| `ui.jsx` Sparkline path/area 进场 | `shouldAnimate = !pref.reduced && !hasDrawn`; reduced 时 duration 0 | 已覆盖 |
| `TrendChart.jsx` path/area 进场 | 同 `ui.jsx`，`shouldAnimate` 排除 reduced | 已覆盖 |
| `AppMgmtDrawer.jsx` 抽屉开合/拖拽 | `pref.reduced` 时 `x.set(0)`、只做 opacity；`drag=false` | 已覆盖 |
| `AppMgmtDrawer.jsx` backdrop opacity | reduced 时显式 animate opacity，非 reduced 才绑定 `useTransform` | 已覆盖 |
| `AppMgmtDrawer.jsx` 进度条 `edgeProgress` | 全局 reduced-motion animation duration 兜底 | 已覆盖 |
| `Monitoring.jsx` / `Hardware.jsx` / `DiskManager.jsx` 进度条 width transition | 本次补丁统一压缩 transition duration/delay | 已补齐 |
| `AppStore.jsx` / `AppIcon.jsx` / `AlertCenter.jsx` / `LoginScreen.jsx` / `TweaksPanel.jsx` 内联 hover/focus/transform transition | 本次补丁统一压缩 transition duration/delay；`AlertCenter` transform transition 同样被压缩 | 已补齐 |
| `LoginScreen.jsx` / `AuthModal.jsx` loading spinner | 全局 reduced-motion animation duration/iteration 兜底；仍保留加载状态视觉符号 | 已覆盖 |

## 未验证项

- 抽屉活体拖拽：本机无已部署应用入口，`AppWindow` 仅对 `app.kind === 'app'` 渲染“运维信息”按钮；当前桌面/API 只暴露系统工具，因此无法打开真实抽屉。代码层 reduced-motion 分支已审计。
- `prefers-reduced-transparency`：代码层 media 块存在；agent-browser `set media reduced-transparency` 和 CDP `Emulation.setEmulatedMedia` 均未让 `matchMedia('(prefers-reduced-transparency: reduce)')` 变为 true，运行截图不作为通过证据。
