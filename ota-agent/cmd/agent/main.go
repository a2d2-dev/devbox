package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theriseunion/edge-ota/agent/pkg/apps"
	"github.com/theriseunion/edge-ota/agent/pkg/collector"
	"github.com/theriseunion/edge-ota/agent/pkg/config"
	"github.com/theriseunion/edge-ota/agent/pkg/console"
	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"github.com/theriseunion/edge-ota/agent/pkg/mqtt"
	"github.com/theriseunion/edge-ota/agent/pkg/reader"
	"github.com/theriseunion/edge-ota/agent/pkg/reporter"
	"github.com/theriseunion/edge-ota/agent/pkg/writer"
	"go.uber.org/zap"
)

var (
	configFile = flag.String("config", "", "Path to config file")
	version    = "dev" // 由编译时注入
)

func main() {
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	logger, err := initLogger(cfg.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("OTA Agent starting",
		zap.String("version", version),
		zap.String("device", cfg.Device.Name))

	// 全局 context + heartbeat ticker（MQTT goroutine 和 console 共用）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// MQTT 连接异步启动，不阻塞控制台
	go func() {
		mqttClient, err := mqtt.New(&mqtt.Config{
			Broker:   cfg.GetMQTTBroker(),
			ClientID: cfg.GetClientID(),
			CAFile:   cfg.MQTT.CAFile,
			DeviceID: cfg.Device.Name,
			Logger:   logger,
		})
		if err != nil {
			logger.Warn("Failed to create MQTT client, OTA commands disabled", zap.Error(err))
			return
		}

		if err := mqttClient.Connect(); err != nil {
			logger.Warn("Failed to connect to MQTT broker, OTA commands disabled", zap.Error(err))
			return
		}

		logger.Info("MQTT connected, enabling OTA commands")

		taskExecutor := executor.New(logger)
		fileReader := reader.New(logger)
		fileWriter := writer.New(logger)
		resultReporter := reporter.New(mqttClient, logger)

		commandTopic := mqttClient.GetDeviceCommandTopic()
		mqttClient.Subscribe(commandTopic, func(topic string, payload []byte) error {
			return handleCommand(logger, taskExecutor, resultReporter, topic, payload)
		})
		execTopic := mqttClient.GetDeviceExecTopic()
		mqttClient.Subscribe(execTopic, func(topic string, payload []byte) error {
			return handleExecCommand(logger, taskExecutor, mqttClient, topic, payload)
		})
		readTopic := mqttClient.GetDeviceReadTopic()
		mqttClient.Subscribe(readTopic, func(topic string, payload []byte) error {
			return handleReadCommand(logger, fileReader, mqttClient, topic, payload)
		})
		writeTopic := mqttClient.GetDeviceWriteTopic()
		mqttClient.Subscribe(writeTopic, func(topic string, payload []byte) error {
			return handleWriteCommand(logger, fileWriter, mqttClient, topic, payload)
		})

		// 心跳
		if err := mqttClient.PublishHeartbeat(); err != nil {
			logger.Error("Failed to publish heartbeat", zap.Error(err))
		}
		for {
			select {
			case <-ctx.Done():
				mqttClient.Disconnect()
				return
			case <-heartbeatTicker.C:
				if err := mqttClient.PublishHeartbeat(); err != nil {
					logger.Error("Failed to publish heartbeat", zap.Error(err))
				}
			}
		}
	}()

	// 启动本地控制台 HTTP 服务器
	if cfg.Console.Enabled {
		col := collector.New(logger, version)
		go col.Start(ctx)

		// 初始化 K8s 应用管理器（可选，失败不阻止启动）
		var appMgr *apps.Manager
		appsCfg := apps.Config{
			Kubeconfig: cfg.Kubernetes.Kubeconfig,
			Namespace:  cfg.Kubernetes.Namespace,
		}
		if mgr, err := apps.NewManager(logger, appsCfg); err != nil {
			logger.Warn("K8s app manager initialization failed, app management disabled",
				zap.Error(err))
		} else {
			appMgr = mgr
		}

		// 初始化应用商店管理器（可选）
		var storeMgr *apps.StoreManager
		if cfg.Kubernetes.APIServerURL != "" {
			storeCfg := apps.StoreConfig{
				APIServerURL: cfg.Kubernetes.APIServerURL,
				ConsoleURL:   cfg.Console.ConsoleURL,
				Kubeconfig:   cfg.Kubernetes.Kubeconfig,
			}
			if mgr, err := apps.NewStoreManager(logger, storeCfg); err != nil {
				logger.Warn("Store manager initialization failed", zap.Error(err))
			} else {
				storeMgr = mgr
			}
		}

		consoleCfg := console.Config{
			Enabled:           cfg.Console.Enabled,
			Port:              cfg.Console.Port,
			StaticDir:         cfg.Console.StaticDir,
			SupervisorSocket:  cfg.Console.SupervisorSocket,
			SupervisorConfDir: cfg.Console.SupervisorConfDir,
		}
		consoleServer := console.NewServer(logger, consoleCfg, col, appMgr, storeMgr)
		go func() {
			if err := consoleServer.Start(ctx); err != nil {
				logger.Error("Console HTTP server failed", zap.Error(err))
			}
		}()
	}

	logger.Info("OTA Agent is ready")

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down OTA Agent...")
	cancel()                           // 停止心跳
	time.Sleep(500 * time.Millisecond) // 等待心跳 goroutine 退出
	logger.Info("OTA Agent stopped")
}

// handleCommand 处理接收到的命令
func handleCommand(
	logger *zap.Logger,
	exec *executor.Executor,
	reporter *reporter.Reporter,
	topic string,
	payload []byte,
) error {
	logger.Info("Command received",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	// 解析任务
	task, err := executor.ParseTask(payload)
	if err != nil {
		logger.Error("Failed to parse task", zap.Error(err))
		return err
	}

	logger.Info("Task parsed",
		zap.String("task_id", task.ID),
		zap.String("type", task.Type))

	// 执行任务（阻塞，串行执行）
	result := exec.Execute(task)

	// 上报结果
	if err := reporter.ReportTaskResult(result); err != nil {
		logger.Error("Failed to report task result",
			zap.String("task_id", task.ID),
			zap.Error(err))
		return err
	}

	return nil
}

// handleExecCommand 处理 exec 类型命令（Story 3.1）
func handleExecCommand(
	logger *zap.Logger,
	exec *executor.Executor,
	mqttClient *mqtt.Client,
	topic string,
	payload []byte,
) error {
	logger.Info("Exec command received",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	// 解析 exec 任务
	task, err := executor.ParseTask(payload)
	if err != nil {
		logger.Error("Failed to parse exec task", zap.Error(err))
		return err
	}

	// 确保任务类型为 exec
	if task.Type == "" {
		task.Type = "exec"
	}

	logger.Info("Exec task parsed",
		zap.String("request_id", task.RequestID),
		zap.String("command", task.Command),
		zap.Int("timeout", task.Timeout))

	// 执行任务（阻塞，串行执行）
	result := exec.Execute(task)

	// 发布结果到 exec/result topic
	resultTopic := mqttClient.GetDeviceExecResultTopic()

	// 序列化结果为 JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		logger.Error("Failed to marshal exec result", zap.Error(err))
		return err
	}

	// 发布到 MQTT
	if err := mqttClient.Publish(resultTopic, resultJSON); err != nil {
		logger.Error("Failed to publish exec result",
			zap.String("request_id", task.RequestID),
			zap.String("topic", resultTopic),
			zap.Error(err))
		return err
	}

	logger.Info("Exec result published",
		zap.String("request_id", task.RequestID),
		zap.String("status", result.Status),
		zap.Int("exit_code", result.ExitCode),
		zap.String("topic", resultTopic))

	return nil
}

// handleReadCommand 处理 read 类型命令（Story 4.1）
// 读取本地文件并返回 base64 编码的内容
func handleReadCommand(
	logger *zap.Logger,
	fileReader *reader.Reader,
	mqttClient *mqtt.Client,
	topic string,
	payload []byte,
) error {
	logger.Info("Read command received",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	// 解析读取请求
	req, err := reader.ParseReadRequest(payload)
	if err != nil {
		logger.Error("Failed to parse read request", zap.Error(err))
		// 发送错误响应
		errorResp := reader.NewErrorResponse("", err.Error())
		return publishReadResult(logger, mqttClient, errorResp)
	}

	logger.Info("Read request parsed",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.Int64("max_size", req.MaxSize))

	// 执行文件读取
	result := fileReader.Read(req)

	// 发布结果到 read/result topic
	return publishReadResult(logger, mqttClient, result)
}

// publishReadResult 发布读取结果到 MQTT
func publishReadResult(
	logger *zap.Logger,
	mqttClient *mqtt.Client,
	result *reader.ReadResponse,
) error {
	resultTopic := mqttClient.GetDeviceReadResultTopic()

	// 序列化结果为 JSON
	resultJSON, err := result.ToJSON()
	if err != nil {
		logger.Error("Failed to marshal read result", zap.Error(err))
		return err
	}

	// 发布到 MQTT
	if err := mqttClient.Publish(resultTopic, resultJSON); err != nil {
		logger.Error("Failed to publish read result",
			zap.String("request_id", result.RequestID),
			zap.String("topic", resultTopic),
			zap.Error(err))
		return err
	}

	// 记录结果信息
	if result.Error != "" {
		logger.Warn("Read result published (error)",
			zap.String("request_id", result.RequestID),
			zap.String("error", result.Error),
			zap.String("topic", resultTopic))
	} else {
		logger.Info("Read result published (success)",
			zap.String("request_id", result.RequestID),
			zap.Int64("size", result.Size),
			zap.String("sha256", result.SHA256),
			zap.String("topic", resultTopic))
	}

	return nil
}

// handleWriteCommand 处理 write 类型命令（Story 4.2）
// 将 base64 编码的内容写入本地文件
func handleWriteCommand(
	logger *zap.Logger,
	fileWriter *writer.Writer,
	mqttClient *mqtt.Client,
	topic string,
	payload []byte,
) error {
	logger.Info("Write command received",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	// 解析写入请求
	req, err := writer.ParseWriteRequest(payload)
	if err != nil {
		logger.Error("Failed to parse write request", zap.Error(err))
		// 发送错误响应
		errorResp := writer.NewErrorResponse("", err.Error())
		return publishWriteResult(logger, mqttClient, errorResp)
	}

	logger.Info("Write request parsed",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.String("mode", req.Mode))

	// 执行文件写入
	result := fileWriter.Write(req)

	// 发布结果到 write/result topic
	return publishWriteResult(logger, mqttClient, result)
}

// publishWriteResult 发布写入结果到 MQTT
func publishWriteResult(
	logger *zap.Logger,
	mqttClient *mqtt.Client,
	result *writer.WriteResponse,
) error {
	resultTopic := mqttClient.GetDeviceWriteResultTopic()

	// 序列化结果为 JSON
	resultJSON, err := result.ToJSON()
	if err != nil {
		logger.Error("Failed to marshal write result", zap.Error(err))
		return err
	}

	// 发布到 MQTT
	if err := mqttClient.Publish(resultTopic, resultJSON); err != nil {
		logger.Error("Failed to publish write result",
			zap.String("request_id", result.RequestID),
			zap.String("topic", resultTopic),
			zap.Error(err))
		return err
	}

	// 记录结果信息
	if result.Error != "" {
		logger.Warn("Write result published (error)",
			zap.String("request_id", result.RequestID),
			zap.String("error", result.Error),
			zap.String("topic", resultTopic))
	} else {
		logger.Info("Write result published (success)",
			zap.String("request_id", result.RequestID),
			zap.String("path", result.Path),
			zap.Int64("size", result.Size),
			zap.String("sha256", result.SHA256),
			zap.String("topic", resultTopic))
	}

	return nil
}

// initLogger 初始化日志记录器
func initLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	// 解析日志级别
	var level zap.AtomicLevel
	switch cfg.Level {
	case "debug":
		level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	// 创建日志配置
	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = level

	// 如果指定了日志文件，输出到文件；否则输出到 stdout
	if cfg.File != "" {
		zapConfig.OutputPaths = []string{cfg.File}
		zapConfig.ErrorOutputPaths = []string{cfg.File}
	}

	return zapConfig.Build()
}
