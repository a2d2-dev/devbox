# OTA Agent

OTA (Over-The-Air) Agent 是运行在边缘设备上的轻量级代理程序，负责：
- 通过 MQTT 连接到云端 NATS Server
- 接收并执行云端下发的任务
- 上报任务执行结果
- 定期发送心跳消息

## 快速开始

### 1. 构建

```bash
# 安装依赖
make deps

# 构建本地平台二进制
make build

# 构建 Linux x86_64 二进制
make build-linux

# 构建 ARM64 二进制
make build-arm
```

### 2. 配置

复制示例配置文件并修改：

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`，主要配置项：

```yaml
device:
  name: "device-001"  # 设备唯一标识符

mqtt:
  host: "198.19.249.2"  # NATS MQTT 服务器地址
  port: 8883
  ca_file: "/etc/ota-agent/certs/ca.crt"  # CA 证书路径

logging:
  level: "info"
```

### 3. 准备 CA 证书

从 Kubernetes 集群获取 CA 证书：

```bash
# 创建证书目录
sudo mkdir -p /etc/ota-agent/certs

# 获取 CA 证书
kubectl get secret nats-tls -n ota-system -o jsonpath='{.data.ca\.crt}' | \
  base64 -d | sudo tee /etc/ota-agent/certs/ca.crt

# 验证证书
openssl x509 -in /etc/ota-agent/certs/ca.crt -text -noout | grep Subject
```

### 4. 运行

```bash
# 方式 1: 使用 Makefile（自动检查配置文件）
make run

# 方式 2: 直接运行二进制
./bin/ota-agent -config config.yaml

# 方式 3: 使用环境变量覆盖配置
OTA_DEVICE_NAME=device-002 \
OTA_MQTT_HOST=192.168.1.100 \
./bin/ota-agent -config config.yaml
```

## 项目结构

```
ota-agent/
├── cmd/
│   └── agent/
│       └── main.go              # 主程序入口
├── pkg/
│   ├── config/
│   │   └── config.go            # 配置管理
│   ├── mqtt/
│   │   └── client.go            # MQTT 客户端
│   ├── executor/
│   │   └── executor.go          # 任务执行器
│   └── reporter/
│       └── reporter.go          # 结果上报器
├── deployments/                 # 部署配置（预留）
├── config.yaml.example          # 配置文件示例
├── Makefile                     # 构建脚本
├── go.mod                       # Go 模块定义
└── README.md                    # 本文档
```

## 功能说明

### MQTT 主题规范

#### 1. 心跳主题（Agent → Cloud）
```
ota/devices/{device_id}/heartbeat
```

**Payload**:
```json
{
  "timestamp": "2025-12-14T10:30:00Z",
  "status": "online"
}
```

**频率**: 30 秒

#### 2. 命令主题（Cloud → Agent）
```
ota/devices/{device_id}/commands/#
```

**示例任务 Payload**:
```json
{
  "id": "task-001",
  "type": "shell",
  "command": "echo 'Hello from cloud' > /tmp/test.txt",
  "timeout": 60
}
```

#### 3. 结果主题（Agent → Cloud）
```
ota/devices/{device_id}/results/{task_id}
```

**Payload**:
```json
{
  "task_id": "task-001",
  "status": "success",
  "output": "Hello from cloud\n",
  "error": "",
  "start_time": "2025-12-14T10:30:00Z",
  "end_time": "2025-12-14T10:30:01Z",
  "duration": 0.15
}
```

### 任务类型

#### 1. Shell 命令（Ad-Hoc）
```json
{
  "id": "task-shell-001",
  "type": "shell",
  "command": "uptime",
  "timeout": 30
}
```

#### 2. Playbook（后期实现）
```json
{
  "id": "task-playbook-001",
  "type": "playbook",
  "playbook_ref": "registry.example.com/ota/playbooks/nginx:1.0.0",
  "timeout": 600
}
```

## 测试

### 手动测试（使用 mosquitto 客户端）

#### 1. 订阅心跳主题
```bash
mosquitto_sub \
  -h 198.19.249.2 \
  -p 8883 \
  -t "ota/devices/+/heartbeat" \
  --cafile /etc/ota-agent/certs/ca.crt \
  -v
```

**期望输出**:
```
ota/devices/device-001/heartbeat {"timestamp":"2025-12-14T10:30:00Z","status":"online"}
```

#### 2. 订阅结果主题
```bash
mosquitto_sub \
  -h 198.19.249.2 \
  -p 8883 \
  -t "ota/devices/+/results/#" \
  --cafile /etc/ota-agent/certs/ca.crt \
  -v
```

#### 3. 发送测试任务
```bash
mosquitto_pub \
  -h 198.19.249.2 \
  -p 8883 \
  -t "ota/devices/device-001/commands/shell" \
  -m '{"id":"task-001","type":"shell","command":"uptime","timeout":30}' \
  --cafile /etc/ota-agent/certs/ca.crt
```

**期望输出**（在结果订阅终端）:
```json
{
  "task_id": "task-001",
  "status": "success",
  "output": " 10:30:15 up  5:23,  1 user,  load average: 0.15, 0.20, 0.18\n",
  "error": "",
  "start_time": "2025-12-14T10:30:15Z",
  "end_time": "2025-12-14T10:30:15Z",
  "duration": 0.05
}
```

## 故障排查

### 1. MQTT 连接失败

**问题**: `Failed to connect to MQTT broker`

**检查步骤**:
1. 验证 NATS Server 运行正常
   ```bash
   kubectl get pods -n ota-system
   ```

2. 验证 LoadBalancer External IP
   ```bash
   kubectl get svc nats-mqtt -n ota-system
   ```

3. 测试网络连通性
   ```bash
   nc -zv 198.19.249.2 8883
   ```

4. 验证 CA 证书
   ```bash
   openssl x509 -in /etc/ota-agent/certs/ca.crt -text -noout
   ```

### 2. 任务执行失败

**问题**: 任务状态显示 `failed`

**检查步骤**:
1. 查看 Agent 日志
   ```bash
   # 如果配置了日志文件
   tail -f /var/log/ota-agent.log

   # 否则查看 stdout
   ```

2. 验证命令语法
   ```bash
   # 在边缘设备上手动执行命令
   sh -c "your-command"
   ```

3. 检查超时设置
   - 复杂任务增加 `timeout` 值

### 3. 心跳未收到

**问题**: 云端未收到设备心跳

**检查步骤**:
1. 验证 Agent 运行状态
   ```bash
   ps aux | grep ota-agent
   ```

2. 检查 MQTT 连接状态
   - Agent 日志应显示 "MQTT connected"

3. 订阅心跳主题验证
   ```bash
   mosquitto_sub -h 198.19.249.2 -p 8883 -t "ota/devices/+/heartbeat" --cafile /etc/ota-agent/certs/ca.crt -v
   ```

## 配置优先级

配置加载优先级（从高到低）：
1. **环境变量**: `OTA_<SECTION>_<KEY>` (例如: `OTA_DEVICE_NAME`)
2. **配置文件**: `config.yaml` 或 `-config` 参数指定的文件
3. **默认值**: 代码中定义的默认值

## 日志级别

- `debug`: 详细调试信息（包含 MQTT 消息详情）
- `info`: 一般信息（连接状态、任务执行）
- `warn`: 警告信息（连接丢失、重连）
- `error`: 错误信息（任务执行失败、配置错误）

## 后期优化方向

- [ ] 集成 go-ansible 库替换 exec.Command
- [ ] 实现 Playbook 任务类型
- [ ] 添加 ORAS 客户端支持
- [ ] 实现任务并发执行（可配置 max_concurrent_tasks）
- [ ] 添加离线任务队列支持
- [ ] 实现设备状态上报（CPU、内存、磁盘）
- [ ] 添加 Prometheus metrics 暴露

## License

Copyright © 2025 EdgePlatform
