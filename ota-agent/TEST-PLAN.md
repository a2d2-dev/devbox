# OTA Agent 综合测试计划

**版本**: v1.0
**日期**: 2025-12-14
**目标**: 全面验证 OTA Agent 的功能、性能和可靠性

---

## 测试环境

### 基础设施
- **NATS Server**: 198.19.249.2:8883 (MQTT over TLS)
- **Agent 版本**: v1.1.0
- **测试平台**: macOS arm64
- **Go 版本**: 1.21+

### 测试工具
- `test-send-command.go` - 单命令测试工具
- `test-multiple-commands.go` - 多命令测试工具
- `test-agent-scenarios.go` - 综合场景测试工具（待创建）

---

## 测试场景

### 场景 1: 基础连接测试

**目标**: 验证 Agent 能够正确连接到 MQTT Broker

**步骤**:
1. 启动 Agent
2. 验证 TLS 连接成功
3. 验证心跳消息发送
4. 验证订阅主题成功

**期望结果**:
- ✅ Agent 日志显示 "Successfully connected to MQTT broker"
- ✅ 心跳消息每 30 秒发送一次
- ✅ 订阅 `ota/devices/device-001/commands/#` 成功

**验收标准**:
```json
{
  "test": "基础连接测试",
  "pass_criteria": {
    "connection_time": "< 5s",
    "heartbeat_interval": "30s ± 1s",
    "subscription_success": true
  }
}
```

---

### 场景 2: Shell 命令执行测试

**目标**: 验证各类 Shell 命令的正确执行

**测试用例**:

#### 2.1 简单命令
```json
{
  "id": "test-simple-001",
  "type": "shell",
  "command": "echo 'Hello OTA'",
  "timeout": 10
}
```
**期望输出**: `"Hello OTA\n"`

#### 2.2 长时间命令
```json
{
  "id": "test-long-002",
  "type": "shell",
  "command": "sleep 3 && echo 'done'",
  "timeout": 10
}
```
**期望输出**: `"done\n"` (耗时 ~3s)

#### 2.3 管道命令
```json
{
  "id": "test-pipe-003",
  "type": "shell",
  "command": "ls -la /tmp | head -5",
  "timeout": 10
}
```
**期望输出**: 前 5 行 /tmp 目录列表

#### 2.4 变量引用
```json
{
  "id": "test-var-004",
  "type": "shell",
  "command": "USER_HOME=$HOME && echo $USER_HOME",
  "timeout": 10
}
```
**期望输出**: 用户 HOME 目录路径

**验收标准**:
- ✅ 所有命令成功执行
- ✅ 输出内容正确
- ✅ 执行时间合理
- ✅ Status 为 "success"

---

### 场景 3: 错误处理测试

**目标**: 验证各类错误的正确处理

**测试用例**:

#### 3.1 命令不存在
```json
{
  "id": "test-error-001",
  "type": "shell",
  "command": "nonexistent-command-12345",
  "timeout": 10
}
```
**期望结果**:
```json
{
  "status": "failed",
  "error": "exec: \"sh\": executable file not found in $PATH OR command not found"
}
```

#### 3.2 命令超时
```json
{
  "id": "test-timeout-002",
  "type": "shell",
  "command": "sleep 100",
  "timeout": 2
}
```
**期望结果**:
```json
{
  "status": "failed",
  "error": "command timeout after 2 seconds"
}
```

#### 3.3 命令返回非零退出码
```json
{
  "id": "test-exitcode-003",
  "type": "shell",
  "command": "exit 1",
  "timeout": 10
}
```
**期望结果**:
```json
{
  "status": "failed",
  "error": "exit status 1"
}
```

#### 3.4 空命令
```json
{
  "id": "test-empty-004",
  "type": "shell",
  "command": "",
  "timeout": 10
}
```
**期望结果**: 任务应该失败或返回空输出

**验收标准**:
- ✅ 所有错误都被正确捕获
- ✅ Error 字段包含明确的错误信息
- ✅ Status 为 "failed"
- ✅ 不会导致 Agent 崩溃

---

### 场景 4: 并发任务测试

**目标**: 验证 Agent 处理多个并发任务的能力

**测试步骤**:
1. 快速连续发送 10 个任务
2. 每个任务执行时间 1-3 秒随机
3. 验证所有任务都被正确处理

**任务示例**:
```json
[
  {"id": "concurrent-001", "command": "sleep 1 && echo task1"},
  {"id": "concurrent-002", "command": "sleep 2 && echo task2"},
  {"id": "concurrent-003", "command": "sleep 1 && echo task3"},
  ...
  {"id": "concurrent-010", "command": "sleep 3 && echo task10"}
]
```

**验收标准**:
- ✅ 所有 10 个任务都返回结果
- ✅ 结果顺序可能不同（异步执行）
- ✅ 无任务丢失
- ✅ 无任务重复执行
- ✅ 总耗时 < 5 秒（并发执行）

---

### 场景 5: 离线重连测试

**目标**: 验证 Agent 的自动重连机制

**测试步骤**:
1. 启动 Agent，验证连接成功
2. 停止 NATS Server Pod
3. 观察 Agent 日志（应显示重连尝试）
4. 等待 30-60 秒
5. 重启 NATS Server Pod
6. 验证 Agent 自动重连成功
7. 发送测试命令，验证功能恢复

**验收标准**:
- ✅ Agent 检测到连接断开
- ✅ Agent 自动开始重连（日志显示 "MQTT reconnecting..."）
- ✅ NATS 恢复后，Agent 在 30 秒内重连成功
- ✅ 重连后功能完全正常
- ✅ 未丢失任何心跳或命令

---

### 场景 6: 长时间运行测试

**目标**: 验证 Agent 的稳定性和资源管理

**测试步骤**:
1. 启动 Agent
2. 持续运行 1 小时
3. 每分钟发送一个测试命令
4. 监控内存和 CPU 使用率
5. 检查日志是否有异常

**验收标准**:
- ✅ Agent 稳定运行 1 小时无崩溃
- ✅ 内存使用稳定（无内存泄漏）
- ✅ CPU 使用率正常（< 5% 空闲，< 50% 执行任务）
- ✅ 所有 60 个命令都成功执行
- ✅ 日志无 ERROR 级别消息

---

### 场景 7: 心跳机制测试

**目标**: 验证心跳消息的正确性和稳定性

**测试步骤**:
1. 启动 Agent
2. 订阅心跳主题 `ota/devices/device-001/heartbeat`
3. 记录 10 个心跳消息的时间戳
4. 计算心跳间隔

**期望心跳格式**:
```json
{
  "timestamp": "2025-12-14T21:11:24Z",
  "status": "online"
}
```

**验收标准**:
- ✅ 心跳间隔为 30 秒 ± 1 秒
- ✅ 心跳 JSON 格式正确
- ✅ timestamp 格式为 RFC3339
- ✅ status 为 "online"

---

## 性能基准

### 命令执行性能

| 命令类型 | 期望耗时 | 最大耗时 |
|---------|---------|---------|
| echo    | < 50ms  | 200ms   |
| uptime  | < 100ms | 500ms   |
| ls      | < 200ms | 1s      |
| sleep 3 | ~3s     | 3.5s    |

### 网络性能

| 指标 | 期望值 | 最大值 |
|-----|-------|-------|
| 连接建立时间 | < 2s | 5s |
| 消息发送延迟 | < 100ms | 500ms |
| 重连时间 | < 10s | 30s |

---

## 测试执行记录

| 场景 | 执行日期 | 状态 | 备注 |
|-----|---------|------|------|
| 场景 1 | 2025-12-14 | ✅ PASS | - |
| 场景 2 | - | ⏳ TODO | - |
| 场景 3 | - | ⏳ TODO | - |
| 场景 4 | - | ⏳ TODO | - |
| 场景 5 | - | ⏳ TODO | - |
| 场景 6 | - | ⏳ TODO | - |
| 场景 7 | - | ⏳ TODO | - |

---

## 问题跟踪

| ID | 场景 | 问题描述 | 状态 | 解决方案 |
|----|-----|---------|------|---------|
| - | - | - | - | - |

---

## 测试工具使用

### 单命令测试
```bash
cd edge-ota/agent
GOSUMDB=off GOPROXY=direct go run test-send-command.go
```

### 多命令测试
```bash
cd edge-ota/agent
GOSUMDB=off GOPROXY=direct go run test-multiple-commands.go
```

### 场景测试（待创建）
```bash
cd edge-ota/agent
GOSUMDB=off GOPROXY=direct go run test-agent-scenarios.go --scenario=all
```

---

## 总结与改进建议

**当前状态**: Phase 1, Day 2-3 完成

**已验证**:
- ✅ 基础连接
- ✅ 简单命令执行
- ✅ 多命令并发

**待测试**:
- ⏳ 错误处理边界场景
- ⏳ 离线重连机制
- ⏳ 长时间稳定性
- ⏳ 性能基准测试

**改进建议**:
1. 添加自动化测试框架
2. 集成 Prometheus 指标监控
3. 添加单元测试覆盖率
4. 实现测试报告自动生成
