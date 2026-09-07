# Issue #34 真深色主题验证报告

## 实验目的

验证 console-ui 支持 light / dark / system 三种主题：深色主题真实渲染桌面、窗口、个人设置、系统设置、文件与日志页面；system 跟随浏览器系统配色实时切换；主题偏好仍通过既有 `/api/v1/account/preferences` 路径同步。

## 实验步骤

1. 在 `console-ui` 执行 `npm ci` 安装依赖。
2. 执行 `npm test` 与 `npm run build`。
3. 启动临时 mock API：`http://10.126.126.12:19134`，用于免登录前端截图与只读数据桩。
4. 启动 Vite：`DEVBOX_API_TARGET=http://10.126.126.12:19134 npm run dev -- --host 10.126.126.12 --port 5174 --strictPort`。
5. 使用 agent-browser 打开 `http://10.126.126.12:5174/`，采集浅色、深色、system 媒体仿真截图。
6. 注入测试 token 后切换主题，检查偏好同步请求。

## 实验记录

- `npm test`：通过。Vitest `17 passed (17)`，`64 passed (64)`；Node compose 测试 `7` 个通过。
- `npm run build`：通过。Vite `489 modules transformed`，产物生成于 `console-ui/dist`，该目录不提交。
- 深色主题变量：`data-theme=dark`，`--edge-bg=#0f131a`，`--edge-surface=#171c25`。
- system 跟随验证：
  - `agent-browser set media dark` 后：`data-theme=dark`，`data-theme-preference=system`，`--edge-bg=#0f131a`。
  - `agent-browser set media light` 后：`data-theme=light`，`data-theme-preference=system`，`--edge-bg=#f3f3f3`。
- 偏好同步：有 `edge_token` 时切换主题触发 `PUT http://10.126.126.12:5174/api/v1/account/preferences`，返回 `200`。
- 浏览器错误：`agent-browser errors` 无输出。

## 截图清单

- `01-light-desktop.png`：浅色桌面。
- `02-dark-desktop.png`：深色桌面。
- `03-dark-account-profile.png`：深色个人设置 / 我的账号。
- `04-dark-account-appearance-dark-selected.png`：深色个人设置 / 主题壁纸 / 深色选中。
- `05-dark-account-devices.png`：深色个人设置 / 登录设备。
- `06-appearance-light-selected.png`：主题壁纸 / 浅色选中。
- `07-appearance-system-selected.png`：主题壁纸 / 跟随系统选中。
- `08-system-emulated-dark.png`：system + 系统深色仿真。
- `09-system-emulated-light.png`：system + 系统浅色仿真。
- `10-dark-system-settings.png`：深色系统设置。
- `11-dark-files.png`：深色文件页面。
- `12-dark-audit-log.png`：深色日志页面。

## 备注

截图使用临时 mock API 完成，因为本地真实后端 `10.126.126.12:9092` 未监听且真实登录凭据不可用。mock 仅用于前端渲染与偏好请求路径验证；后端代码未修改。
