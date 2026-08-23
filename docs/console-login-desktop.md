# DevBox 登录、桌面引导与实时状态

本文记录 GitHub Issue #10 落地后的控制台行为与后端契约。

## 登录会话

- 控制台启动时调用 `GET /api/v1/auth/status` 校验已保存的 token。服务端确认有效后才恢复桌面；无效或请求失败时清理浏览器 token 并返回登录页。
- “保持至服务端会话过期”把 token 写入 `localStorage`；“仅本次浏览器会话”写入 `sessionStorage`。两者都受后端 `auth.session_ttl` 限制，浏览器不会延长服务端有效期。
- 任一受保护接口返回认证 `401` 后，前端立即清理 token 并只触发一次退出通知。仍在运行的轮询不会形成登录页回跳循环。
- 未启用 `auth.password` 时，`/auth/status` 会允许无 token 会话，API 请求也不再被前端短路。
- 忘记密码不提供邮件找回。管理员在主机上修改生效配置的 `auth.password`，或设置 `DEVBOX_AUTH_PASSWORD`，再按当前部署方式重启 DevBox；进程重启会使旧 token 失效。

## 首次使用引导

引导包含四个稳定步骤：

| ID | 含义 | 操作入口 |
| --- | --- | --- |
| `storage` | 初始化存储工作区 | 磁盘管理 |
| `recommendedApps` | 选择推荐应用 | 应用商店 |
| `remoteAccess` | 确认远程访问方式 | 服务导航 |
| `securityContact` | 保存安全联系邮箱 | 系统设置 |

每个步骤状态为 `pending`、`completed` 或 `skipped`。桌面只展示第一个 `pending` 步骤；完成步骤不会再次出现；只要存在已跳过步骤就显示恢复入口。状态通过受认证的 `GET/PATCH /api/v1/onboarding` 读写。

默认持久化文件为 `/etc/devbox/onboarding.json`。若配置了 `console.browser_data_path`，引导文件与浏览器数据文件放在同一目录，文件名固定为 `onboarding.json`。写入使用 `0600` 权限和临时文件原子替换。

安全联系步骤只有提交有效邮箱后才能标记完成，也可以跳过。存储就绪状态来自 `console.work_dir` 对应目录的真实文件系统检查；配置为空时沿用文件浏览器的 `/data` 默认值。

## 桌面实时状态

桌面状态组件每 5 秒独立刷新以下只读接口：

| 接口 | 数据 |
| --- | --- |
| `GET /api/v1/desktop/status/cpu` | CPU 使用率 |
| `GET /api/v1/desktop/status/memory` | 内存使用率与容量 |
| `GET /api/v1/desktop/status/network` | 累计收发字节，前端按相邻采样计算速率 |
| `GET /api/v1/desktop/status/storage` | 聚合磁盘读写速率与工作区就绪状态 |
| `GET /api/v1/desktop/status/uptime` | 运行秒数和可读时长 |

每张卡维护自己的加载、上次成功数据和错误状态。单个接口失败只影响对应卡片。点击状态组件打开现有“监控”窗口；存储未就绪时的操作按钮打开“磁盘”窗口。

“已部署应用”为空时桌面显示“服务未配置”，并提供应用商店入口。该状态来自真实 `/api/v1/apps` 结果，不使用前端 mock 或 fallback 应用。
