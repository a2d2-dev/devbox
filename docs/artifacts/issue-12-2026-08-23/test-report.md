# Issue #12 用户管理与硬件概览验收报告

## 实验目的

验证控制台多用户 CRUD 与角色边界、旧单管理员密码兼容、用户组读写、只读硬盘与挂载概览，以及前后端构建结果。DevBox 定位为桌面算力平台，本次未验证也未实现 RAID、分区、格式化、SSD 缓存、硬盘休眠或存储空间向导。

## 实验步骤

1. 运行 Go 构建、重点包测试、排除已知 `pkg/apps` 环境问题后的全仓测试。
2. 运行 console-ui 测试和 `make ui`，确认构建产物同步到 `pkg/console/dist`。
3. 用 supervisor 启动当前 worktree 的 `bin/devbox-issue12`，监听 `0.0.0.0:9097`。
4. 用 agent-browser 访问 `http://10.126.126.12:9097`，通过现有 `auth.password` 登录。
5. 查看用户列表；创建用户组并关联已有普通用户，确认列表立即回显。
6. 创建普通用户 `qauser`，退出管理员并用新用户真实登录；打开用户管理，确认接口返回 `403` 且 UI 显示权限提示。
7. 查看硬件中心存储页，同时采集 `lsblk`、`findmnt`、`df` 与 `smartctl` 可用性进行对照。

## 实验记录

### 构建与自动测试

| 项目 | 结果 | 记录 |
| --- | --- | --- |
| `go build ./...` | 通过 | 无编译错误 |
| `go test ./pkg/users ./pkg/auth ./pkg/hardware ./pkg/console` | 通过 | 用户、认证、硬件、控制台重点包全部通过 |
| 排除 `pkg/apps` 的全仓 Go 测试 | 通过 | `go list ./... \| rg -v '/pkg/apps$' \| xargs go test` |
| `go test ./...` | 未通过 | `pkg/apps.TestRepoMigratePreservesCatalogSourceAcrossReopen` 在 modernc SQLite `Xfsync` 阻塞并于 10 分钟超时；其余已输出包通过 |
| `npm test` | 通过 | Vitest 3 个文件、30 个测试通过；Node 7 个测试通过 |
| `make ui` | 通过 | Vite 构建完成，dist 已同步到 `pkg/console/dist` |
| `npx eslint src/pages/Users.jsx` | 通过 | 本票新增用户管理页面无 lint 错误 |
| `git diff --check` | 通过 | 无空白错误 |

新增的 HTTP 级测试覆盖弱密码 `400`、大小写重名 `409`、禁用/删除最后管理员 `409`、旧 password-only 请求登录成功。用户组测试覆盖单 SQLite 连接下成员列表读取，防止结果集嵌套查询自锁。

### 浏览器行为

- 旧配置兼容：现有 `auth.password` 通过真实登录页和 `/api/v1/auth/verify` 成功进入桌面。
- 新用户登录：UI 创建 `qauser` 后，退出管理员并使用该账户成功进入桌面。
- 角色边界：普通用户请求 `/api/v1/users` 的浏览器网络记录为 `403`；页面显示“当前账户不是管理员，无法访问用户管理”。
- 用户组：创建“开发团队”并关联 1 名成员后，用户组列表立即显示 1 人，没有加载阻塞。
- agent-browser 的 `errors` 与 `console` 检查未发现页面错误。

### 硬件对照

| 设备 | `lsblk` 字节数 | 页面显示 | 介质 / 接口 |
| --- | ---: | ---: | --- |
| `/dev/sda` | 250059350016 | 232.89 GiB | SSD / SATA |
| `/dev/sdb` | 4000787030016 | 3.64 TiB | HDD / SATA |
| `/dev/nvme0n1` | 2000398934016 | 1.82 TiB | NVMe / NVME |

页面列出的 `/`、`/boot`、`/data`、`/data/_ssd`、`/data/_ssd/vm-images-fast`、`/data/_ssd2t` 与 `findmnt --real` 的来源、文件系统、容量和使用率一致。主机未安装 `smartctl`，三块盘均明确显示“不支持 / smartctl 未安装”，没有显示空值或伪造健康值。

## 截图证据

- [用户列表](users-list.png)
- [用户组](user-groups.png)
- [硬盘与挂载概览](hardware-storage.png)
- [普通用户 403 边界](normal-user-403.png)

原始截图同时保留在 `/tmp/issue-12-evidence/`。

## 遗留问题

- 全量 `go test ./...` 仍受 `pkg/apps` 已知文件系统 `fsync` 阻塞影响；本票允许跳过，该包不在 Issue #12 改动范围。
- 全仓 `npm run lint` 当前报告 147 个错误、9 个警告，分布在多处既有页面与组件；本票未做越界清理。Issue #12 新增的 `Users.jsx` 单文件 lint 已通过。
- SMART 实际健康结果未验证，因为本机没有 `smartctl`；“工具不可用”状态已真实验证。
