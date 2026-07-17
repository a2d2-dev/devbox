# T07 reduced-motion 审计与最终验证报告

日期：2026-07-17
分支：`feat/apple-design`
验证地址：`http://10.126.126.12:5181`
说明：现有 `5174` Vite 代理默认指向 `9090`，而本机 `9090` 为 mihomo；本轮另启前台 Vite，并显式设置 `DEVBOX_API_TARGET=http://10.126.126.12:8080`。

## 实验目的

1. 全量审计 `console-ui/src` 内动画、transition、motion spring、hover/tap 动效是否具备 reduced-motion 降级。
2. 用 agent-browser 验证窗口、Dock、Toast、Tab、图表页面在普通模式和 reduced-motion 模式下的核心交互。
3. 产出截图证据与未验证项说明，完成 T07 收官票。

## 实验步骤

1. 执行 `rg -n "animation|transition|whileHover|whileTap|springs\.|animate" console-ui/src`，逐项核对降级分支。
2. 补齐 `styles.css` reduced-motion 全局 transition 降级。
3. 运行 `npm run build`、`npm run lint`，最终确认再执行 `npx eslint .`。
4. 用 agent-browser `--session t07` 登录本机控制台，完成窗口最大化连点、两窗口 Dock 切换、Toast、reduced-motion、页面截图。
5. 尝试模拟 `prefers-reduced-transparency`，并记录工具限制。

## 实验记录

### 审计结论

- 审计清单：`docs/artifacts/apple-design-2026-07-16/reduced-motion-audit.md`
- 发现缺失：1 类，内联 CSS transition 缺少统一 reduced-motion 降级。
- 已修复：`console-ui/src/styles.css`，在 reduced-motion media 块中新增全局 `transition-duration: 0.001ms !important` 和 `transition-delay: 0ms !important`。

### 构建与 lint

- `npm run build`：通过。Vite 输出 chunk 大小提示为现状 warning。
- `npm run lint` / `npx eslint .`：失败，现状 293 problems（283 errors, 10 warnings）。该数值与任务给定“分支现状 293”一致；main 存量 302，本分支净减 9，T07 未新增 lint 问题。

### 截图证据

- 桌面：`docs/artifacts/apple-design-2026-07-16/t07-desktop.png`
- 窗口 framed：`docs/artifacts/apple-design-2026-07-16/t07-window-framed.png`
- 窗口最大化：`docs/artifacts/apple-design-2026-07-16/t07-window-maximized.png`
- 状态栏菜单：`docs/artifacts/apple-design-2026-07-16/t07-statusbar-menu.png`
- Dock hover：`docs/artifacts/apple-design-2026-07-16/t07-dock-hover.png`
- AIActivity：`docs/artifacts/apple-design-2026-07-16/t07-ai-activity.png`
- Monitoring：`docs/artifacts/apple-design-2026-07-16/t07-monitoring.png`
- Toast 进场：`docs/artifacts/apple-design-2026-07-16/t07-toast-enter.png`
- Toast 3 连堆叠：`docs/artifacts/apple-design-2026-07-16/t07-toast-stack.png`
- reduced-motion 窗口：`docs/artifacts/apple-design-2026-07-16/t07-reduced-window.png`
- reduced-motion Toast：`docs/artifacts/apple-design-2026-07-16/t07-reduced-toast.png`
- reduced-motion Tab：`docs/artifacts/apple-design-2026-07-16/t07-reduced-tabs.png`
- reduced-transparency 尝试截图：`docs/artifacts/apple-design-2026-07-16/t07-reduced-transparency.png`（模拟未生效，仅作失败尝试证据）

### 交互压测

- 快速连点最大化按钮 3 次：通过。使用当前窗口 rect 动态计算最大化按钮坐标，采样显示窗口从最大化连续过渡到 framed，最终窗口仍存在且可交互；未见跳变或卡死。
- 两窗口从 Dock 切换：通过。监控窗口激活时，仪表盘窗口为 `opacity: 0`、`visibility: hidden`、`pointer-events: none`、`zIndex: 20`；活动窗口为 `zIndex: 21` 且可交互，淡出窗口不遮挡。
- Toast：通过。Files 中点击“复制路径”触发 Toast；单次进场截图已保存；连续点击 3 个复制按钮形成堆叠；等待 3.6s 后 DOM 中 Toast 文案计数为 0，退场完成。
- reduced-motion：通过。`matchMedia('(prefers-reduced-motion: reduce)') === true`；窗口与 Tab 截图保存，采样显示活动窗口 `transform: none`，transition duration 被压到 `1e-06s`；Toast reduced 截图保存。
- prefers-reduced-transparency：未验证。agent-browser `set media light reduced-transparency` 未改变 `matchMedia`；CDP `Emulation.setEmulatedMedia` 也未作用到 agent-browser 当前执行上下文。代码层 `styles.css` 已存在 media 块，运行层未确认。

### 历史遗留项

- Toast 动画：已覆盖，见 `t07-toast-enter.png`、`t07-toast-stack.png` 与退场 DOM 计数。
- 抽屉活体拖拽：未验证。本机没有已部署应用入口；代码中“运维信息”按钮仅对 `app.kind === 'app'` 渲染，当前桌面只暴露系统工具。代码层已审计 `pref.reduced` 禁拖拽和 opacity 降级。
- 终端类应用最小化状态保活：未做 UI 验证。桌面没有 terminal 入口；代码层 `App.jsx` 保持 open apps 挂载，`AppWindow` 用 `visible/minimized` 控制显示，符合保活设计。

## 遗留问题

1. `prefers-reduced-transparency` 需要一个能真实模拟该媒体特性的浏览器环境复验。
2. 抽屉甩动关闭需要本机部署至少一个 `kind: 'app'` 应用后复验。
3. lint 仍有分支既有 293 个问题，T07 未扩大该问题面。
