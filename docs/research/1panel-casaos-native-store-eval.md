# 原生应用商店兼容：1Panel / CasaOS 协议研究与转换评估

面向 devbox Issue #2 / PR #3「原生应用商店兼容」目标，研究 1Panel 与 CasaOS（ZimaOS）官方应用商店仓库的真实目录结构与元数据格式，评估能否在当前 Compose 应用模型中可靠、安全地转换为统一 StoreApp / StoreAppVersion。

> **证据标记**（沿用 `docs/research/self-hosted-app-management-platforms.md` 约定）
>
> - **可确认事实**：能在官方 GitHub 仓库 / 官方 spec 文档中直接确认。
> - **推断 / 设计启示**：从可确认事实推出的设计判断，不声称是官方承诺。
> - **未确认**：未在本次一手来源中确认；不等于产品不支持。

---

## 检索方法与 smart-search 摘要

- 检索时间：**2026-07-23**。
- smart-search 预检（主会话）：`opencli list -f yaml`、`opencli github -h`。结论：实时 registry 的 GitHub 适配器**只有 login / whoami，无代码搜索能力**。
- **opencli GitHub 实际代码搜索次数：0（能力缺失）。**
- 因此本研究的全部一手证据来自**官方 GitHub 仓库与 REST API 直接核验**（`gh api repos/.../contents`、官方 spec Markdown），未使用 opencli 代码搜索，未使用二手博客。

### 一手来源 URL

- 1Panel 应用商店仓库：https://github.com/1Panel-dev/appstore
- 1Panel 打包规范（AI skills）：https://github.com/1Panel-dev/1Panel-appstore-skills
- 1Panel 主仓库（反馈入口）：https://github.com/1Panel-dev/1Panel
- CasaOS / ZimaOS 应用商店仓库：https://github.com/IceWhaleTech/CasaOS-AppStore
- CasaOS 应用管理 API：https://github.com/IceWhaleTech/CasaOS-AppManagement
- CasaOS compose + x-casaos spec：https://github.com/IceWhaleTech/CasaOS-AppStore/blob/main/docs/specs/compose-and-x-casaos.md
- CasaOS build-output spec：https://github.com/IceWhaleTech/CasaOS-AppStore/blob/main/docs/specs/build-output.md
- **1Panel 官方格式规范（权威）**：https://github.com/1Panel-dev/1Panel-appstore-skills/blob/main/references/appstore-format.md
- **CasaOS v2 协议规范（中文）**：https://github.com/IceWhaleTech/CasaOS-AppStore/tree/main/docs/zh/specs

### 仓库体量与默认分支（可确认，决定 clone 策略）

- `1Panel-dev/appstore`：`size=355122 KiB`（~347 MB），**`default_branch=dev`**（不是 main）。
- `IceWhaleTech/CasaOS-AppStore`：`size=358449 KiB`（~350 MB）。
- （推断）仓库总体积约 347 MB；采用 partial clone + sparse-checkout 只取 `data.yml` / `docker-compose.yml`，可在协议层避开 logo / 截图 / 脚本等大资产，无需整体下载（未逐文件量化体积占比，故标推断）。
- **结论**：不能用完整 shallow clone + 既有 64/128 MiB 总目录限额（官方源会直接不可用）。原生商店 Git adapter 必须用 **partial clone `--filter=blob:none` + non-cone sparse-checkout**，只取所需文本文件（见下「clone 策略」）。
- **实测验证（2026-07-23，可确认）**：`git clone --depth 1 --filter=blob:none --no-checkout https://github.com/1Panel-dev/appstore.git`（不指定 ref）自动 HEAD=dev；写入 non-cone patterns `/data.yaml`、`/apps/*/data.yml`、`/apps/*/*/data.yml`、`/apps/*/*/docker-compose.yml` 后 checkout：`.git` 1180 KiB、apps worktree 4360 KiB、共 996 个文件、**非 data.yml/docker-compose.yml 文件为 0**、约 8.7 s（临时目录已清理）。实现走 `git sparse-checkout set --no-cone --stdin`，patterns 经 stdin、命令 argv，不经 shell。

---

## 1Panel 应用商店格式（可确认事实）

仓库 `1Panel-dev/appstore`，默认分支 `dev`（API 可确认：`default_branch=dev`）。**源文件即运行时格式**：1Panel 直接读取本仓库目录，无构建步骤。

### 顶层

```
appstore/
  data.yaml          # 商店名 / 标题 / 分类标签（含 i18n locales）
  apps/              # 约 260 个应用（`gh api repos/1Panel-dev/appstore/contents/apps | length`，2026-07-23 计数），扁平结构 apps/<app-key>/
  README.md / README_zh.md
  logo.png, renovate.json, LICENSE
```

- 顶层 `data.yaml` 示例（可确认）：`name: 1Panel`、`additionalProperties.tags[]`（每项 `key/name/sort/locales{en,ja,ms,pt-br,ru,ko,zh-hant,zh,tr,es-es,fa}`）。

### 应用级 `apps/<app-key>/data.yml`（可确认，例 `apps/adguardhome/data.yml`）

```yaml
name: AdGuardHome
tags: [安全]
title: ...
description: ...
additionalProperties:
  key: adguardhome            # == 目录名，稳定应用标识
  name: AdGuardHome
  tags: [Security]
  shortDescZh / shortDescEn
  description: {i18n map}
  type: website
  crossVersionUpdate: true
  limit: 0
  batchInstallSupport: true
  website: https://...
  github: https://...
  document: https://...
  architectures: [amd64, arm/v6, arm/v7, arm64, ppc64le]
```
另有 `logo.png`、`README.md`、`README_en.md`，以及**版本子目录**（如 `0.107.78/`）。

### 版本级 `apps/<app-key>/<version>/`（可确认）

每个版本目录至少包含：

- `data.yml`：**表单 schema**，`additionalProperties.formFields[]`，每字段：
  - `envKey`（如 `PANEL_APP_PORT_HTTP_1`、`APP_KEY`、`PANEL_REDIS_ROOT_PASSWORD`）
  - `type`：`text` / `number` / `password` / `select`（可确认见 `text/number/password`）
  - `rule`：`paramPort` / `paramExtUrl` / …（校验规则）
  - `default`、`required`、`edit`、`label{en,zh,...}` / `labelEn` / `labelZh`
- `docker-compose.yml`：引用 `${envKey}` 变量。
- 部分版本还含 `data/`（种子数据）、`scripts/init.sh`（宿主初始化脚本）。

`docker-compose.yml` 示例（`apps/adguardhome/0.107.78/docker-compose.yml`，可确认）：

```yaml
services:
  adguardhome:
    container_name: ${CONTAINER_NAME}
    restart: always
    networks: [1panel-network]
    ports:
      - ${PANEL_APP_PORT_DNS}:53/tcp
      - ${PANEL_APP_PORT_HTTP}:3000/tcp
      # ...
    volumes:
      - ./data/work:/opt/adguardhome/work
      - ./data/conf:/opt/adguardhome/conf
    image: adguard/adguardhome:v0.107.78
    labels:
      createdBy: "Apps"
networks:
  1panel-network:
    external: true
```

`scripts/init.sh` 示例（`apps/2fauth/8.0.1/scripts/init.sh`，可确认）：`#!/bin/bash` + `chown -R 1000:1000 data`（宿主侧命令）。

### 1Panel 关键事实小结

- 镜像标签为版本化（`adguard/adguardhome:v0.107.78`），非 `latest`。
- 端口/密码等参数通过 `formFields` → `${envKey}` 注入 compose；password 字段存在明文默认值（如 `PANEL_REDIS_ROOT_PASSWORD` default `redis`）。
- compose 假设存在外部网络 `1panel-network`、由 1Panel 注入 `${CONTAINER_NAME}`、相对 bind `./data/...`。

---

## CasaOS / ZimaOS 应用商店格式（可确认事实）

仓库 `IceWhaleTech/CasaOS-AppStore`，默认分支 `main`，`store-config.json` 显示 `version: 2`、`store_id: zimaos-appstore`（即 **ZimaOS v2 应用商店**）。

### 顶层

```
CasaOS-AppStore/
  Apps/                       # 源: Apps/<AppName>/docker-compose.yml + icon/screenshot
  build/scripts/              # 构建脚本
  docs/specs/                 # compose-and-x-casaos.md / build-output.md
  store-config.json           # version/store_id/name(i18n)
  category-list.json, featured-apps.json, recommend-list.json
  supported-languages.json
  .env.example
```

**关键：仓库中 `dist/` 目录不存在（HTTP 404）——构建产物未提交。** `build-output.md` 明确「protocol is consumed from generated files under `dist/`」（store.json / index.json / `dist/apps/<id>/docker-compose.yml` + `meta.json` + assets）。即：**源是创作格式，运行时消费的是 CI 生成的 `dist/`**。

### 源 `Apps/<AppName>/docker-compose.yml`（可确认，例 `Apps/AdGuardHome`）

单文件 compose + 顶层与服务级 `x-casaos` 扩展：

```yaml
name: adguard-home
services:
  adguard-home:
    image: adguard/adguardhome:v0.107.76
    network_mode: bridge
    ports: [{target: 53, published: "531", protocol: tcp}, ...]
    volumes:
      - {type: bind, source: /DATA/AppData/$AppID/opt/adguardhome/work, target: /opt/adguardhome/work}
    container_name: adguard-home
    x-casaos:
      ports: [{container: "53", description: {en_US: ""}}, ...]
      volumes: [{container: /opt/adguardhome/work, description: {en_US: ""}}]
x-casaos:
  id: org.icewhale.adguardhome            # 稳定商店身份
  architectures: [386, amd64, arm, arm64, ppc64le]
  main: adguard-home                       # 主 UI 服务
  author: CasaOS Team
  category: Networking
  description: {en_US, zh_CN, ar_SA, de_DE, ...}   # 大段 i18n
  tagline: {...}
  title: {...}
  version: "0.107.76"
```

另有 `icon.png/icon.svg/thumbnail.png/screenshot-*.png`（截图单张可达 ~800KB）。

---

## 转换评估

### 1Panel → devbox StoreApp/StoreAppVersion（推断：可靠，本 PR 实现）

| 1Panel | devbox 映射 |
|---|---|
| `apps/<key>/data.yml` `additionalProperties.key` | StoreApp.ID（`source/<key>`） |
| `name` / `description` / `shortDescZh` / `tags` / `logo.png` / `website` / `github` | StoreApp 展示字段 + category |
| 版本目录名 `<version>` | StoreAppVersion.Version |
| 版本 `data.yml` `formFields[]` | `valuesSchema.fields[]`（key/type/required/default/label/rule） |
| `formFields` 默认值 + 用户输入 | `.env`（0600），走既有 secret 脱敏 |
| 版本 `docker-compose.yml` | StoreAppVersion.compose（模板/原文，安装时可信重取） |

**安全/语义边界（推断，须在转换层强制）：**

- `networks.1panel-network (external:true)` → **收敛为 project-managed**（不剥离）：保留 network key 与所有服务的 `networks:` 引用，仅去掉 `external: true` 与显式 `name:`，让 Compose 创建 `<project>_1panel-network`（devbox-<app> project 内，多 service 仍互通）。**绝不创建/复用全局共享 1panel-network。** 若存在 `1panel-network` 以外的 external network（依赖 1Panel 外部 DB/面板/serviceName）→ 标该 version 不可安装，原因写明「依赖未知外部服务」，不猜。
- `${CONTAINER_NAME}` / `container_name` → **移除**（含 `${CONTAINER_NAME}-<svc>` 派生）；devbox 按 `devbox-<id>` project 管理（避免冲突 / 命名劫持）。
- 相对 bind `./data/...` → 落到 devbox 该应用 data_dir（devbox 已有此模型）；仍走既有风险策略（系统关键 bind / `..` traversal 阻断）。
- **`scripts/init.sh` 本 PR 绝不执行**（不新增 shell 解析）：1Panel `init.sh` 是宿主侧任意 shell（如 `chown -R`），devbox 绝不执行上游脚本。存在 `scripts/init.sh` 的 version **保守标记不可安装**，原因写明「上游提供宿主初始化脚本，devbox 不执行；数据目录权限可能不足」（不伪装可安装）。未来若支持仅限严格白名单相对 managed path 的 Go 声明式 chown，非本次范围。
- password 字段明文默认值（如 `redis`）→ 走既有「敏感明文 / 明文默认值阻断」规则；要求用户显式提供或标记不可安装。
- 镜像 `latest/main/...` → 既有可变标签阻断（1Panel 多为版本化，符合）。

### CasaOS → devbox（推断：本 PR 不做，明确边界）

**结论：源 `x-casaos` 虽内联可解析，但本 PR 不实现 CasaOS 原生转换。** 理由（均有可确认事实支撑，非臆测）：

1. **运行时格式是构建产物，非源**：官方 `build-output.md` 明确协议从 `dist/`（store.json/index.json + 每应用 compose + meta.json）消费，而 `dist/` 未提交。devbox 要么跑 CasaOS 构建脚本（任意脚本执行 + 工具链依赖，安全/可靠性不可接受），要么自行复刻 v2 构建（locale 解析 / 架构 / content_hash / meta 生成，显著拖大范围）。
2. **路径模型不兼容**：volume 用 `/DATA/AppData/$AppID/...`（CasaOS 专属绝对路径 + `$AppID` 变量），需重写为 devbox 受管路径；与 1Panel 的相对 `./data/...` 是两套语义。
3. **表单/参数模型弱**：`x-casaos` 主要是展示元数据 + ports/volumes 描述，没有 1Panel `formFields` 那样的强类型 schema（type/rule/default/required）；多数应用为「按原样安装」，可配置参数的可靠映射证据不足（未确认）。
4. **资源体积**：仓库含大量截图（单张 ~800KB），浅克隆成本高；需按需经 API 取资产，增加适配复杂度。
5. **第二套独立适配器**：可靠 + 安全 + 带真实 fixture 测试的 CasaOS 适配器是独立工作量，超出本 PR「原生 1Panel」主目标，会显著拖大范围。

**因此本 PR：** 复用现有 `Catalog` 接口（`Kind()` 即格式 seam）与 `NewCatalog` 按 Kind 分派，新增 `onepanel` 实现一个 `Catalog`；**不为 CasaOS 预先引入新抽象接口**（karpathy-guidelines：避免为单一未来用例做投机抽象，现有 seam 已足够）。CasaOS 留作后续 PR，记录上述前置条件与所需工作（复刻/绕过构建、路径重写、参数模型、资产按需取）。这符合用户裁决「不可可靠转换则明确边界，不能猜」。

### 本轮不在范围（未完成协议评估，不作开闭源断言）

- fnOS、Unraid：本轮不在范围，且未完成其协议评估；**不对其作开源/闭源断言**（用户裁决：本轮只需 1Panel；其他不开源的不兼容；其他开源如 CasaOS 可评估）。

---

## 给实现的约束清单（1Panel）

1. 后端新增 `1panel` 原生适配器：解析顶层 `data.yaml`（分类）+ 每个 `apps/<key>/data.yml`（应用元数据）+ 每个版本目录 `data.yml`（formFields）+ `docker-compose.yml`。
2. 类型探测：目录含 `apps/*/data.yml` + 顶层 `data.yaml(name:1Panel)` 即判 `1panel`；也支持显式 `1panel`。
3. **Clone 用 partial + sparse（非完整 shallow）**：`--filter=blob:none --no-checkout --sparse` + `sparse-checkout set --no-cone`，只取 `/data.yaml`、`/apps/*/data.yml`、`/apps/*/*/data.yml`、`/apps/*/*/docker-compose.yml`；**不拉 logo/截图/脚本二进制**。仍限 fetched/working tree 大小、文件数、单文件大小、超时；token 经 http.extraHeader 注入并脱敏；symlink/traversal 复用 `safeReadCatalogFile`。**scripts/init.sh 存在性**通过 partial clone 已有的 tree object 用 `git ls-tree -r --name-only HEAD` 路径列表判断（**不拉脚本 blob**），ls-tree 输出限制字节/路径数防恶意 repo。不支持 partial clone 的服务 → **清晰错误，不回退完整 clone**（会突破体积限额）。
4. **空 ref 不硬编码 main**：空 ref 时用 remote HEAD（1Panel 官方默认分支为 `dev`）；显式 ref 才 `--branch` 固定。
5. 转 StoreApp/StoreAppVersion；安装按 `sourceId+appId+version` 可信重取 → Controller.Apply 风险策略；保留上游 source/app/version。
6. 单来源失败隔离 + last-good cache；不兼容条目（latest、blocked 权限、明文 secret 默认、缺 compose、依赖 init 脚本、依赖未知外部网络）显示明确原因。
7. **持久化复用同一 `apps.db`（不新建第二 SQLite 文件）**：扩展现有 `Repository/migrate` 加 `catalog_sources` 表，来源变更复用现有 `audit`；DB 文件权限 0600。YAML 来源 `managedBy=config`（只读），DB 来源 `managedBy=database`（可编辑/启停/删除）；同 ID 冲突 YAML 优先、DB create 返 409；token 只写不回显（更新时空 token = 不变）。
8. 真实上游 fixture 测试（用 1Panel-dev/appstore 真实结构造最小 fixture，含 port/password/select 字段、scripts/init.sh、external `1panel-network` 多 service、`${CONTAINER_NAME}`）。
