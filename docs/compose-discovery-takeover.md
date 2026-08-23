# 系统 Compose project 自动发现与安全接管

Issue #2 新增目标（2026-07-29）：「Compose 应用」列表自动发现 Docker daemon 中由 Docker Compose
创建的外部 project（含 stopped 容器），与 devbox 受管应用统一展示；登录用户显式「接管并编辑」后
进入既有编辑 / 版本 / 日志 / 生命周期 / 卸载边界。

## 发现（只读）

- `composeRuntime.Observe` 按 `com.docker.compose.project` 聚合 daemon 上**所有**仍有容器记录的
  project（`listContainers all=true`）。controller 与受管 meta 去重后，未被覆盖的 project 作为
  **discovered（只读）**展示——包括未登记的 `devbox-*` project（prefix 非所有权证据）。
- **列表扫描绝不读取** `working_dir`/`config_files` 指向的宿主文件；只读容器 labels 中的 project /
  service / image / state / ports，以及 `working_dir`/`config_files` 的**路径字符串**（诊断展示）。
- discovered 用稳定、互不冲突的 ID（`ExternalID`/`DiscoveredAltID` + `shortHash(project)`，详见下文）。
  List/Get/Takeover 共用同一 deterministic discovery index（单次 snapshot：一次 metas + 一次 compose
  Observe + 一次 k8s Observe），claimed 集含受管 meta ID + K8s observed ID，避免 discovered 与 K8s
  app 碰撞。discovered 卡片标记「已发现 · 只读」，隐藏全部写操作（start/stop/restart/redeploy/uninstall
  以及详情 drawer），只展示服务清单（name/state/health/ports）+ 来源路径诊断 + 接管入口。
- 写操作对未接管 project 一律返回 `not_managed`（HTTP 400）。

## 接管（Takeover）

显式 `POST /api/v1/apps/{id}/takeover`（body 仅 `confirmRisky`；硬解码：始终 decode、EOF=空、
非法 400、超限 413、`DisallowUnknownFields`、拒 trailing JSON）。安全顺序：

1. **路径安全（openat2）**：working_dir 用 `openat2(RESOLVE_NO_SYMLINKS)` 取 dirfd；每个 config file
   用 `openat2(dirfd, RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS|NO_XDEV)` 打开——安全决定与打开**原子**完成，
   消除「先 Lstat/EvalSymlinks 再 ReadFile」的 TOCTOU（final/父目录在检查与打开间被换成 symlink 的攻击
   失效），并拒绝通过 working_dir 内的子 bind mount 跨到其它文件系统。`fstat` 确认 regular/size，
   从 fd 有界读取并校验读取长度 == fstat size（文件并发变化则拒绝）。
   非 Linux 明确拒绝（capability 错误），不退化为不安全读。working_dir 必须绝对、非系统敏感根/祖先
   （`/`、`/etc`、`/proc`、`/sys`、`/dev`、`/run`、`/var/run`、`/boot`、`/usr`、`/bin`、`/sbin`、`/lib*`）、
   非 devbox data dir / docker socket 目录（或其祖先/之下）。错误只说「路径策略拒绝」，不回显内容。
2. **文件访问预扫描**：在任何 compose CLI 调用前，对每个已安全读取的 body 跑 `AnalyzeComposeFileAccess`，
   阻断 `include`/`extends.file`/`env_file`/`secrets.file`/`configs.file`/`build`（否则 CLI 会读取额外宿主文件）。
   每个 config file 顶层须为非空 YAML mapping，整个集合至少一个文件有非空 services（override 文件可只含
   networks/volumes）；include-only 集合被预扫描阻断。
3. **归一化（消除 TOCTOU + 阻止读 .env）**：把已校验 bodies 写到 devbox 控制 0700 临时目录（0600 副本
   + 受控空 `.env`），CLI 只接收这些临时副本 + 显式空 `--env-file` + `--project-directory canonicalWork`
   （保留相对 bind 语义）。`compose config --no-interpolate` 合并多文件为单一规范 YAML（**绝不只复制首文件**），
   变量引用 `${VAR}` 保留——secret 不进正文。
4. **插值门**：托管目录无 `.env`，归一化副本中未提供安全默认值的 `${VAR}`/`$VAR` 必须阻断（禁止静默
   复制/读取宿主 `.env` secret），提示改用粘贴/上传导入向导重新填写参数/secret；`${VAR:-非空}` 放行。
5. **风险预检**：在临时托管环境用现有 `RenderConfig`（空 env）展开安全默认值后，复用既有风险策略
   （literal secret / 宿主文件读取 / 结构风险）；blocked 不可 override，confirmation 需 `confirmRisky` + 审计。
6. **原子持久化**：staging dir 写 `compose.yaml` + `revisions/1.yaml` + takeover marker，fsync（含目录）后
   `rename` 到 AppDir（final 不存在），**再**提交 DB——无「DB 成功但事实源缺失」窗口。崩溃在 rename 后/DB 前
   留下带 marker 的 orphan dir，重试按 marker(project+hash) 复用或冲突，绝不盲删。审计 detail 不含宿主路径
   （仅 project/configCount/hash/confirmation）。

### 原地管理（数据不变）

接管保留原 compose project name 原地管理（`AppRecord.OriginalProject` + `ComposeProjectName(meta)`）；
compose runtime 的 Apply/Operate/Remove/Logs/projectEmpty 与 worker 健康检查统一从内部 `RuntimeProject`
（`json:"-"`，不复用 K8s 的 Namespace）解析真实 project。容器名 / 网络名 / named volume 按 project name
键控，保留原名 = 不重建 = **named volume 数据不变**。`TaskTypeTakeover`（同步 succeeded）准确记录 operation
history，summary 含 `confirmed=true/false`（事务内留痕）；`ObservedRevision=0` 直到真实 redeploy/health 验证。

### 稳定 ID 与冲突消解

- `ExternalID(project)=ext-<slug>-<hash(project)>`、`DiscoveredAltID=discovered-<slug>-<hash(project)>`（与
  `CatalogLocalAppID` 同构，始终含原始 project name 的 shortHash，故 `a_b`/`a-b` 不碰撞）。`resolveDiscoveredID`
  在 claimed 约束下选主/副/有界 salt 候选，全占返回空串（调用方按冲突处理）。**不全局禁止 `ext-` 前缀**：
  历史合法受管 app（含 ext- 开头）照常使用；与 discovered 冲突时双方都展示、discovered 回退第二候选、ID 稳定。

### 幂等

锁在最前；同 `Idempotency-Key` + 同 `(target,confirm)` → 返回原 task.app；同 key 异 target/confirm → 409
（在「已受管」早返回之前判定，避免绕过全局 key 冲突）。

## 子进程环境隔离（全生命周期）

`composeCLI.command` 构造的 docker 子进程：daemon 端点走全局参数 `-H`（**不**用 `DOCKER_HOST` env）；
`cmd.Env` 仅含固定非敏感 `PATH` + locale（`LANG`/`LC_*`），**不**含 `HOME`/`DOCKER_CONFIG`/`SSL_CERT*`/
代理 / `DOCKER_HOST` / 任意业务 secret。故 compose 插值 `${HOME}`/`${DOCKER_HOST}`/`${TOKEN}` 均无法读取
devbox 进程的值——`.env` 是 workload 插值的唯一用户值来源（真实 docker 验证 `${HOME:-safe}`→`safe`）。

## 已知边界

- 完全 `compose down` 且容器记录被删除的外部 project 无法从 daemon 自动发现；UI/文档说明，可改用粘贴/上传导入。
- 多文件 Compose 经 `compose config` 归一化为单一托管 `compose.yaml`（无损合并，非仅复制首文件）；语义记录在
  revision note。相对 bind 路径在接管时按 canonical working_dir 解析；接管后 redeploy 走 devbox 托管副本。
- 接管要求 Linux（openat2）；非 Linux 返回 capability 错误。
