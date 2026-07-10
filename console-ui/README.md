# Edge Agent Console UI

边缘设备本地管理控制台前端，运行在设备上，通过 OTA Agent 提供的 HTTP API 展示设备状态、应用、监控等信息。

## 前置条件

- **Node.js** >= 18（推荐 22+）
- **pnpm**（`npm install -g pnpm`）
- **OTA Agent 二进制**（提供后端 API，端口 9091）
- **Go 1.24**（仅编译 Agent 时需要）

## 快速开始

### 1. 编译 OTA Agent（后端）

```bash
cd agent/
make build          # 产出 bin/ota-agent
```

> 如果本机没有 Go 环境，可以直接使用预编译的二进制。

### 2. 准备 Agent 配置

复制示例配置并按环境修改：

```bash
cp agent/config-local.yaml agent/my-config.yaml
```

配置文件关键字段：

```yaml
device:
  name: "your-device-name"     # 设备标识，需与云端注册一致

mqtt:
  host: "114.119.174.108"      # NATS MQTT 服务器地址
  port: 31883                  # MQTT NodePort 端口
  ca_file: ""                  # TLS 时填写 CA 证书路径

console:
  enabled: true
  port: 9091                   # Agent HTTP API 端口
  static_dir: ""               # 留空 = 不托管静态文件（开发模式）
  console_url: "http://114.119.174.108:30080"  # 云端控制台地址

logging:
  level: "debug"
```

### 3. 启动 Agent

```bash
./agent/bin/ota-agent --config agent/my-config.yaml
```

验证 Agent 运行：

```bash
curl http://localhost:9091/api/v1/device
# 应返回设备信息 JSON
```

### 4. 安装前端依赖并启动

```bash
cd agent/console-ui/
pnpm install
pnpm dev -- --host    # --host 允许局域网访问
```

Vite 开发服务器启动在 `http://localhost:5173`，所有 `/api/*` 请求自动代理到 `http://localhost:9091`。

### 5. 访问

- 本机：`http://localhost:5173`
- 局域网：`http://<设备IP>:5173`

## 生产构建

```bash
cd agent/console-ui/
pnpm build           # 产出到 dist/
```

构建产物可通过 Agent 托管：在配置中设置 `console.static_dir` 指向 `dist/` 目录的绝对路径，Agent 会在 9091 端口同时提供 API 和静态文件。

## 项目结构

```
console-ui/
├── src/
│   ├── App.jsx              # 主入口，路由和布局
│   ├── hooks/useApi.js      # 所有 API 数据获取 hooks
│   ├── pages/               # 各功能页面
│   │   ├── Dashboard.jsx    # 仪表盘（CPU/GPU/内存/磁盘）
│   │   ├── AlertCenter.jsx  # 告警中心
│   │   ├── AppStore.jsx     # 应用商店
│   │   ├── Models.jsx       # 模型仓库
│   │   ├── Files.jsx        # 文件管理
│   │   └── Ports.jsx        # 端口与公网访问
│   ├── components/          # 共享组件
│   │   ├── AppShell.jsx     # 左侧导航 + 布局
│   │   └── ui.jsx           # 基础 UI 组件
│   ├── tokens.js            # 设计 token（颜色、间距）
│   ├── icons.jsx            # SVG 图标组件
│   └── data/mock.js         # Mock 数据（仅开发参考，不被引用）
├── vite.config.js           # Vite 配置（代理、插件）
└── package.json
```

## API 代理

开发模式下 Vite 自动代理以下路径到 Agent（`:9091`）：

| 路径 | 说明 |
|------|------|
| `/api/*` | 所有 REST API（设备、指标、应用、文件等） |
| `/auth/*` | 认证相关 |
| `/terminal.html` | Web 终端（WebSocket） |
| `/app-icons/*` | 应用图标静态资源 |

## 常见问题

### 页面空白 / 数据全为空

Agent 未运行或未连接。检查：

```bash
# 1. Agent 进程是否存活
ps aux | grep ota-agent

# 2. API 是否可达
curl http://localhost:9091/api/v1/device

# 3. MQTT 连接是否正常（查看 Agent 日志）
```

### 局域网其他机器无法访问

确保启动时加了 `--host` 参数：

```bash
pnpm dev -- --host
```

### 端口冲突

- Agent 默认 9091，可在配置文件 `console.port` 修改
- Vite 默认 5173，可通过 `pnpm dev -- --port 3000` 修改
- 修改 Agent 端口后需同步修改 `vite.config.js` 中的 proxy target
