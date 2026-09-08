# 桌面收纳批1(P0)验收报告

日期：2026-09-08  
环境：`http://10.126.126.12:5174/` Vite 前端，API 代理到本 worktree 临时启动的 devbox 后端 `10.126.126.12:9093`。  
数据来源：真实 `/api/v1/apps`，认证用临时配置关闭；未使用前端 mock。

## 实验目的

验证批1 P0 的桌面密度整改和低频入口降噪：

- 默认图标尺寸从 `md` 调整为 `sm`，缩小 AppIcon 外层宽度，调整 grid gap、workstation 右栏和 dock 避让。
- `account` 从桌面移除，但保留顶栏头像菜单进入个人设置。
- `browser` 从桌面移除，但保留 dock 固定入口。
- 桌面“已部署应用”只展示运行中的应用，并提供“全部 N 个”入口跳转 Compose 应用管理页。
- 浅色和深色主题下均可渲染。

## 实验步骤

1. 读取调研基线 `.survey-ref/report.md` 与主 checkout spec。
2. 运行 `npm test` 覆盖前端单测和 `src/lib/compose.test.js`。
3. 运行 `npm run build` 检查生产构建。
4. 用 agent-browser 打开 `http://10.126.126.12:5174/`，视口设为 `1280x720`。
5. 清理浏览器 localStorage 后验证默认浅色主题和默认 `sm` 图标密度。
6. 采集 DOM 矩形，检查桌面图标与 dock 是否相交。
7. 展开顶栏头像菜单，点击“个人设置”验证 `account` 路由仍可打开。
8. 点击 dock 中“浏览器”验证 `browser` 路由仍可打开。
9. 切换深色主题后重复桌面截图和 DOM 量测。
10. 点击“全部 5 个”验证 Compose 应用管理页可达并包含完整部署应用列表。

## 记录

### API 数据

真实 `/api/v1/apps` 返回 5 个部署应用：

- `running`: 3 个，`claude-code-hub`、`new-api`、`tokenhub-demo`
- `pending`: 1 个，`frpc`
- `stopped`: 1 个，`happy-server-deploy`

### 1280x720 DOM 量测

浅色与深色量测一致：

- 桌面 AppIcon 总数：24
- 系统入口图标：21
- 部署应用图标：3，仅展示 `state === "running"`
- `account` 桌面图标：不存在
- `browser` 桌面图标：不存在
- “全部 5 个”入口：存在
- dock 矩形：`x=573 y=640 width=135 height=62 bottom=702`
- 系统入口最大 bottom：428
- 部署应用最大 bottom：613
- 图标与 dock 相交数：0

说明：调研基线为 28 个桌面图标。本批按明确 scope 移除 `account/browser` 并把 5 个部署应用收敛为 3 个运行中应用，实测为 `23 - 2 + 3 = 24`。验收描述中的“约 18/19”与当前源码固定 23 个系统入口的算术不一致；未做批2功能聚合。

### Compose 完整列表

从桌面“全部 5 个”入口打开 Compose 应用管理页后，页面文本包含：

- `claude-code-hub`
- `frpc`
- `happy-server-deploy`
- `new-api`
- `tokenhub-demo`

## 证据文件

- `before-desktop-full.png`：调研基线截图副本
- `desktop-light.png`：整改后浅色桌面
- `desktop-dark.png`：整改后深色桌面
- `avatar-menu.png`：头像菜单展开，含“个人设置”
- `account-from-avatar.png`：通过头像菜单打开个人设置
- `dock.png`：dock 区域，含浏览器固定入口
- `browser-from-dock.png`：通过 dock 打开浏览器
- `compose-all-apps-entry.png`：通过“全部 5 个”进入 Compose 应用管理页
- `dom-measure-after-light.json`：浅色 DOM 量测
- `dom-measure-after-dark.json`：深色 DOM 量测
- `compose-entry-check.json`：Compose 完整列表检查

## 命令结果

- `npm test`：通过。Vitest 18 个测试文件、66 条测试通过；`node --test src/lib/compose.test.js` 7 条测试通过。
- `npm run build`：通过。Vite 构建成功，保留既有大 chunk 警告。
- `npm run lint`：未通过。ESLint 全仓扫描报告 120 个 error、9 个 warning，主要为既有 no-unused-vars、react-refresh 和 react-hooks/set-state-in-effect 问题；未在本批 scope 内清理。
