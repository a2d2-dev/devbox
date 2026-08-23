# A2D2 Devbox

`devbox` 是 **A2D2 (Agentic Agile Driven Development)** 项目家族的单机控制面板，运行在开发者本机上，为人和 AI agent 提供统一的开发环境管理入口。

## 功能

- **Supervisor 面板** —— 通过 supervisord 托管的进程：启停、保活、查看日志、端口归属
- **应用市场 / 应用管理** —— Docker Compose 与本机 Kubernetes 双运行时；支持内联 Compose、平台商店和第三方 HTTP/Git catalog（可选）
- **本地模型** —— 模型目录扫描、容量、可用 runtime 视图
- **文件浏览器** —— 工作区文件浏览
- **Web 终端** —— 浏览器内交互式 shell
- **端口与隧道** —— 本机暴露端口的统一视图
- **观测与告警** —— CPU / 内存 / GPU 指标采集与本地告警规则

## 快速开始

### 1. 构建

```bash
make deps
make build              # 产出 bin/devbox
make build-linux        # 交叉编译到 Linux x86_64
make build-arm          # 交叉编译到 Linux ARM64
```

### 2. 配置

```bash
cp config.yaml.example config.yaml
$EDITOR config.yaml
```

配置文件查找顺序：

1. `--config <path>` 命令行参数指定的路径
2. `/etc/devbox/config.yaml`
3. `$HOME/.devbox/config.yaml`
4. 当前工作目录下的 `config.yaml`

任何字段都可通过 `DEVBOX_*` 环境变量覆盖，例如：

```bash
DEVBOX_CONSOLE_PORT=18080 ./bin/devbox
```

### 3. 运行

```bash
make run                # 等价于 ./bin/devbox -config config.yaml
```

默认监听 `:9090`，浏览器打开即可使用 Web 控制台。

## 项目结构

```
cmd/devbox/             入口 main.go
pkg/
  config/               配置加载与校验
  console/              本地 HTTP / WebSocket 服务（控制台后端）
  supervisor/           supervisord 客户端封装
  apps/                 Compose/K8s 应用领域、异步任务与应用市场
  collector/            系统 / GPU / 设备指标采集
  alerts/               本地告警规则引擎
  files/                文件浏览
  models/               本地模型扫描
  auth/                 控制台鉴权
console-ui/             Web 控制台前端（React + Vite）
```

## 开发

```bash
make test               # 跑单元测试
make test-coverage      # 生成覆盖率报告

# 前端开发
cd console-ui && npm install && npm run dev
```

## License

[Apache License 2.0](./LICENSE)
