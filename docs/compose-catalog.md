# Docker Compose Catalog 接入规范

应用中心的视图、分类、来源信任、安装前影响预检、手动安装和状态规则见
[`app-center.md`](app-center.md)。本文件继续描述 catalog 协议与 Compose 安全约束。

devbox 可以聚合 edge-apiserver 应用市场、`devbox/v1` HTTP/Git catalog，以及原生 1Panel 开源应用商店。第三方包安装时，后端会按 `sourceId + appId + version` 从可信 source 重新读取定义，前端不能提交 Compose 模板原文。

## 添加 1Panel 应用市场

推荐在「应用中心 → 设置 → Catalog 来源」点击 `+`：

1. 填写 Git 仓库地址，例如官方源 `https://github.com/1Panel-dev/appstore`。
2. 格式选择「自动识别」或「1Panel」；分支留空读取远端默认分支（官方源当前为 `dev`）。
3. 私有兼容仓库可填写只读 Token。Token 只写入权限受限的 `apps.db`，API/UI 只返回 `tokenConfigured`。
4. 先测试连接，再保存。来源可停用、启用、刷新或删除；删除来源不删除已安装应用。

动态来源只允许解析到公网地址的 HTTPS Git URL，阻断 loopback、RFC1918、link-local/云元数据地址。内网自建源应由运维写入启动配置。配置来源在 UI 中只读，同 ID 的数据库来源不能覆盖它。

```yaml
compose:
  catalogs:
    - id: onepanel-official
      name: 1Panel 官方商店
      kind: 1panel
      url: https://github.com/1Panel-dev/appstore
```

1Panel adapter 使用 `--filter=blob:none` + non-cone sparse checkout，只获取 `data.yaml`、应用/版本 `data.yml` 和 `docker-compose.yml`。devbox 不执行上游 `scripts/init.sh`；依赖宿主初始化脚本的版本会标记为不可安装。`1panel-network` 收敛为应用 project 内网络，不创建全局共享网络。

## Catalog 目录

HTTP source 的 URL 指向一个目录；Git source 的 `path` 指向仓库内目录。该目录必须包含 `catalog.json`：

```json
{
  "apiVersion": "devbox/v1",
  "name": "community",
  "apps": [
    {
      "id": "whoami",
      "name": "Who Am I",
      "description": "展示请求信息",
      "category": "developer-tools",
      "provider": "community",
      "icon": "https://cdn.example.com/whoami.png",
      "versions": [
        {
          "version": "1.10.3",
          "compose": "apps/whoami/compose.yaml",
          "valuesSchema": {
            "version": "1",
            "fields": [
              { "key": "HTTP_PORT", "type": "number", "required": true, "label": { "zh": "HTTP 端口" } },
              { "key": "APP_PASSWORD", "type": "password", "required": true, "label": { "zh": "访问密码" } }
            ]
          },
          "defaultValues": { "HTTP_PORT": 8080 }
        }
      ]
    }
  ]
}
```

`compose` 是相对于 catalog 目录的文件路径，也可改用 `composeTemplate` 内联模板。模板使用 Go `text/template` 的简单字段访问，例如 `{{ .HTTP_PORT }}`；devbox 不注册 shell、文件或自定义函数。password 字段不会传给模板，Compose 应通过 `${APP_PASSWORD}` 从受管 `.env` 读取。

```yaml
services:
  app:
    image: traefik/whoami:1.10.3
    ports:
      - "{{ .HTTP_PORT }}:80"
    environment:
      APP_PASSWORD: ${APP_PASSWORD}
```

## 安全和生命周期

- 公网 HTTP source 必须使用 HTTPS；明文 HTTP 仅允许 localhost/测试环境的显式配置。
- Git source 只接受 HTTP(S) 仓库，执行 shallow clone，并限制超时、输出、目录总大小和符号链接越界。
- Catalog 包必须使用固定镜像。`latest/main/master/edge/nightly` 等可变标签会被拒绝。
- MVP 在运行 `docker compose config` 前拒绝所有可读取额外宿主文件的入口：`build`、`include`、`env_file`、`extends.file`、`configs.file`、`secrets.file`，以及把受管 `.env`、`compose.yaml`、`revisions/`、`secrets/` 挂入容器。第三方包应只使用固定镜像、受管 `.env` 引用和 Compose named volume。
- `privileged`、Docker socket、host network/PID/UTS/userns、全部 Linux capabilities，以及 `/proc`、`/sys`、`/dev`、`/var/lib/docker` 等系统关键路径 bind 属于 blocked 风险，不能绕过。
- 设备直通、敏感 capability、关闭 seccomp/apparmor、IPC host 和普通绝对 bind 属于 confirmation 风险；首次安装返回风险清单，用户显式确认后才可再次提交。external volume、bind 和 socket 的生命周期始终不归应用所有。
- Compose 中疑似 password/token/secret 的环境变量不得写明文，也不得使用带明文默认值的 `${PASSWORD:-literal}`；应使用 `${PASSWORD}` 或 `${PASSWORD:?required}`，并由 password 类型参数写入受管 `.env`。
- Secret 只写入应用 `.env`（0600），不进入 revision、Task、audit、日志或读取响应。
- 预检会展示 service、镜像、端口、卷、网络和 Secret key；Secret 只展示 key，不返回值。
- 预检会提示同一 Compose 内的重复宿主端口、与已登记应用的端口冲突，以及绝对 bind source 与其他受管应用的路径冲突；这些是部署前警告，实际占用仍由 Compose/Docker 在任务中最终校验。
- catalog 暂时不可用时继续展示上次可信缓存；已安装应用的生命周期不依赖 catalog 在线。
- Git refresh 只更新 catalog 缓存，不会自动升级已安装应用；本功能不是 GitOps reconcile。
- 生产装配只允许本机绝对 Unix socket；`DOCKER_HOST`、`DOCKER_CONTEXT` 与 `COMPOSE_*` 不会覆盖 devbox 选择的 daemon/project/file。

## 应用数据与任务语义

- 所有 Apply、start、stop、restart、redeploy、remove 和 definition restore 都返回 `202 + Task`；同一应用的提交与执行串行，不同应用有限并发。
- 相同且已经成功 observed 的 definition 重复 Apply 返回原成功 Task，不制造新 revision；secret 轮换和失败重试不会被 no-op 吞掉。
- 默认卸载只执行 `compose down` 并删除控制面登记，保留 managed volume 和应用目录中的 `compose.yaml`、`.env`、`app.json`、revision 快照；purge 才删除 managed volume 与应用目录。external volume、bind 和 socket 永远不自动删除。
- 应用卸载后可以用同一 ID 重新安装；旧 revision 文件不会被覆盖，未明确保留的旧 `.env`/secret 不会被新安装继承。
- definition restore 恢复旧 Compose 与非敏感参数、保留当前 secret，然后重新部署；它不恢复数据库或应用数据。
- worker 在进程重启时扫描 queued/running Task，并从 task 对应的不可变 revision 快照恢复期望 Compose/.env 后继续，避免在 SQLite 已提交但事实源尚未提升时执行旧定义。

## API

- `GET /api/v1/catalogs`：来源状态及缓存时间。
- `POST /api/v1/catalogs`：显式刷新已配置来源，不接受 URL。
- `GET /api/v1/catalogs/apps`：聚合应用列表。
- `GET /api/v1/catalogs/version?sourceId=&appId=&v=`：版本与参数 schema，不返回 Compose 模板。
- `POST /api/v1/catalogs/install`：提交安装/升级，返回 `202 + Task`。
- `GET /api/v1/catalogs/sources`：来源管理视图（不含 token）。
- `POST /api/v1/catalogs/sources/test`：测试并自动识别来源，不落库。
- `POST /api/v1/catalogs/sources`：测试成功后保存动态来源。
- `PUT/DELETE /api/v1/catalogs/sources/{id}`：编辑、启停或删除数据库来源。
- `POST /api/v1/catalogs/sources/{id}/refresh`：刷新单个来源。
