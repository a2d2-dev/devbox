# 应用中心

应用中心聚合 DevBox 内置平台商店与动态第三方 catalog。商店、catalog 和手动
Compose 最终都进入同一个 `apps.Controller`、revision、Task 和风险校验管线；应用中心
不维护第二套安装状态或生命周期实现。

## 信息架构

- **全部**：展示当前两个来源返回的完整 catalog；手动 Compose 没有 catalog 发布
  元数据，不混入此视图。
- **最新发布**：优先按条目的 `publishedAt` 倒序。平台商店读取
  `app.theriseunion.io/published-at` annotation，缺失时读取 Kubernetes
  `metadata.creationTimestamp`；devbox catalog v1 可直接声明 `publishedAt`。
  1Panel 等未提供发布时间的来源降级按版本号倒序、名称正序，UI 会明确提示降级依据。
- **已安装**：按 `Application.source` 精确匹配 `storeId`，社区来源额外匹配
  `catalogId`，避免同名应用跨来源串状态；`inline`、`git`、`local` 来源的受管
  Compose 作为“手动安装 / 本机自管”条目直接纳入，未接管的 discovered project 不纳入。
- 搜索、分类、来源筛选是交集关系，可以组合使用。

## 分类归一化

不复制 fnOS 的固定十分类。UI 保留无法识别的上游分类，并把常见平台/1Panel 标签
归一化到以下导航：

| 应用中心分类 | 常见上游标签或关键词 |
| --- | --- |
| AI | `ai-inference`、`ai-tools`、`LLM`、人工智能 |
| 影音娱乐 | `media`、`multimedia`、`video`、`audio`、相册 |
| 下载工具 | `download`、`torrent`、`usenet` |
| 备份同步 | `backup`、`sync`、`cloud`、网盘 |
| 开发工具 | `dev-environment`、`development`、`code`、`git` |
| 数据存储 | `database`、`storage`、`mysql`、`postgres`、`redis` |
| 网络服务 | `network`、`proxy`、`dns`、`vpn`、`web server` |
| 实用效率 | `tool`、`utility`、`office`、`productivity`、`middleware` |
| 其他/原标签 | 无分类时为“其他”；未命中规则时保留原标签 |

映射实现在 `console-ui/src/lib/appcenter.js`，新增映射必须同步补单测和本表。

## 来源与信任

| 来源 | `sourceType` | `trustLevel` | UI 文案 |
| --- | --- | --- | --- |
| DevBox 内置平台商店 | `official` | `reviewed` | DevBox 官方 / 官方审核 |
| 动态 HTTP、Git、1Panel catalog | `community` | `unverified` | 社区来源名 / 社区未审核 |
| 手动安装的受管 Compose | `manual` | `user-managed` | 手动安装 / 本机自管 |

“官方审核”不是跳过运行风险检查。两个来源安装 Compose 时都必须从后端可信 source
重取模板，并经过模板参数校验、`docker compose config`、文件访问检查、明文 Secret
检查和 `AnalyzeCompose`。社区标识提醒用户 catalog 内容不由 DevBox 官方维护。

## 安装前预检

详情安装框必须先执行预检，预检通过后才启用安装按钮：

- `POST /api/v1/store/preflight`
- `POST /api/v1/catalogs/preflight`

请求只包含应用/来源/版本和 schema values，不接受 Compose 原文。后端从选定 source
重取 `StoreAppVersion`，调用 `RenderStoreCompose` 后复用 `Controller.Validate`。响应展示
服务与镜像依赖、宿主端口、卷挂载、网络、Secret key 和风险；Secret value 不回显。
商店/catalog 的 `latest`、`main` 等可变镜像标签在预检时即升格为 blocked，与 Apply
策略一致。

应用列表、版本详情和 preflight 由控制台统一登录保护，普通登录用户可以执行只读预检；
store/catalog 安装、更新、卸载以及 catalog 来源写操作必须由管理员执行。

## 手动安装

“手动安装”复用 Compose 管理器已有的新建向导，支持粘贴或上传 `.yml/.yaml`。链路为：

1. 前端读取文本并提交 `/api/v1/apps/validate`；
2. 后端执行 Compose 渲染、端口/路径冲突与风险检查；
3. blocked 风险不可覆盖，confirmation 风险必须显式确认；
4. `/api/v1/apps` 创建 `source.kind=inline` 的异步 Apply Task；
5. Secret 只进入权限为 `0600` 的 `.env`，不进入 revision、Task、审计或响应。

Apply 成功后，应用由 `/api/v1/apps` 返回并出现在应用中心“已安装”视图；该条目只提供
管理、打开和卸载，不会误走 store/catalog 的版本查询或重装接口。

应用中心不支持让浏览器提交任意本地路径或让后端执行上游脚本。Git catalog 必须先在
设置页作为受控来源添加，遵守 HTTPS、SSRF、partial clone、大小和路径边界。

## 状态与管理

卡片状态互斥，优先级为：活动 Apply Task 的“安装中” > 已装且版本落后的“可更新” >
“已安装” > 不可安装的“不兼容” > “未安装”。已安装应用不会因为当前 catalog 包在本机
不可安装而误标为不兼容。

详情页复用现有能力：更新走同源 install 并写新 revision；卸载走 `UninstallDialog` 和
remove preview；有 HTTP(S) endpoint 时显示“打开”，同时保留“管理”入口。Task 展示
校验、解析依赖、拉取镜像、创建服务、等待健康、验证和清理阶段；失败后可重新打开安装
表单重试，或进入卸载预览清理残留。

## 设置

设置页管理动态 catalog 来源，并保存更新检查频率（仅手动、每小时、每 6 小时、每天）
到浏览器 `localStorage`。定时检查只在应用中心打开且用户已登录时刷新 catalog 元数据，
不会自动升级已安装应用。
