# 桌面收纳批2(P1) 验证报告

## 实验目的

验证“文件”和“网络”两组低风险聚合后，桌面系统入口减少，旧功能在聚合壳内可达，旧 app id 路由兼容有自动化测试覆盖，浅色/深色桌面渲染正常。

## 实验步骤

1. 在 `/data/src/github.com/a2d2-dev/devbox-wt/declutter-b2/console-ui` 执行 `npm test`。
2. 执行 `npm run build`。
3. 用临时配置启动真实 devbox 后端到 `10.126.126.12:9092`，Vite 绑定到 `10.126.126.12:5174` 并代理该后端。
4. 使用 `agent-browser` 打开 `http://10.126.126.12:5174/`，采集桌面浅色/深色截图。
5. 在 UI 中打开“文件”，采集文件主体、下载任务、备份任务截图。
6. 在 UI 中打开“网络”，采集连接、服务导航截图。

## 实验记录

- `npm test`：通过。Vitest 20 个测试文件、73 个测试全通过；Node test 7 个子测试全通过。
- `npm run build`：通过。Vite 构建成功；保留既有大 chunk warning。
- 桌面入口：代码侧 `SYSTEM_APPS` 从 23 个减到 19 个；扣除批1已隐藏的 `account`、`browser` 后，桌面可见系统入口为 17 个。批2明确聚合的四个旧入口 `downloads`、`backup`、`network-connections`、`links` 已从桌面配置移除。
- 文件聚合：`Files.jsx` 原本是侧栏结构，本批沿用侧栏，新增“下载任务”“备份任务”。UI 截图中下载任务列表区域渲染，当前下载任务为空；备份任务列表渲染出 1 个任务。
- 网络聚合：`NetworkSecurity.jsx` 原本是 tab 结构，本批新增“连接”“服务导航”两个 tab。UI 截图中连接表格和服务导航表格均渲染。
- 旧 id 兼容：新增 `src/lib/appRoutes.test.js` 覆盖 `downloads -> files:downloads`、`backup -> files:backup`、`network-connections -> network-security:connections`、`links -> network-security:links`。
- 聚合壳定位：新增 `src/pages/AggregatedRoutes.test.jsx` 覆盖 Files 和 NetworkSecurity 的聚合初始 tab 挂载。

## 截图证据

- `desktop-light.png`：浅色桌面。
- `desktop-dark.png`：深色桌面。
- `files-main.png`：文件主体视图。
- `files-downloads.png`：文件 / 下载任务视图。
- `files-backup.png`：文件 / 备份任务视图。
- `network-connections.png`：网络 / 连接 tab。
- `network-links.png`：网络 / 服务导航 tab。
