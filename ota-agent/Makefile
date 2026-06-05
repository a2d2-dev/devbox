.PHONY: build run clean test deps

# 版本信息
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X main.version=$(VERSION)

# 二进制输出路径
BINARY := bin/ota-agent

# Go 编译选项
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# 默认目标
all: build

# 安装依赖
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# 构建
build: deps
	@echo "Building OTA Agent (version: $(VERSION))..."
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags "$(LDFLAGS)" \
		-o $(BINARY) \
		./cmd/agent

# 运行（需要先准备配置文件）
run: build
	@echo "Running OTA Agent..."
	@if [ ! -f config.yaml ]; then \
		echo "Error: config.yaml not found. Copy config.yaml.example and modify it."; \
		exit 1; \
	fi
	$(BINARY) -config config.yaml

# 清理
clean:
	@echo "Cleaning..."
	rm -rf bin/
	go clean

# 测试命令（基于 Dapr 最佳实践）
test: test-unit

# 单元测试
test-unit:
	@echo "Running unit tests..."
	go test -v -race -cover -tags=unit ./pkg/... ./cmd/...

# 集成测试（需要 NATS）
test-integration:
	@echo "Running integration tests..."
	go test -v -race -tags=integration ./tests/integration/... -timeout=20m

# E2E 测试（需要 Kubernetes）
test-e2e:
	@echo "Running E2E tests..."
	go test -v -tags=e2e ./tests/e2e/... -timeout=30m

# 所有测试
test-all: test-unit test-integration test-e2e

# 生成覆盖率报告
test-coverage:
	@echo "Generating coverage report..."
	go test -v -race -cover -coverprofile=coverage.out -tags=unit ./pkg/... ./cmd/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo ""
	@echo "Overall coverage:"
	@go tool cover -func=coverage.out | tail -1

# 构建 Linux 二进制（用于部署到边缘设备）
build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 $(MAKE) build

# 构建 ARM 二进制（用于 ARM 边缘设备）
build-arm:
	@echo "Building for ARM64..."
	GOOS=linux GOARCH=arm64 $(MAKE) build

# 显示帮助
help:
	@echo "OTA Agent Makefile"
	@echo ""
	@echo "Build Targets:"
	@echo "  make deps            - Install dependencies"
	@echo "  make build           - Build binary"
	@echo "  make build-linux     - Build for Linux x86_64"
	@echo "  make build-arm       - Build for ARM64"
	@echo ""
	@echo "Test Targets:"
	@echo "  make test            - Run unit tests (default)"
	@echo "  make test-unit       - Run unit tests with race detector"
	@echo "  make test-integration - Run integration tests (requires NATS)"
	@echo "  make test-e2e        - Run E2E tests (requires K8s)"
	@echo "  make test-all        - Run all tests"
	@echo "  make test-coverage   - Generate coverage report"
	@echo ""
	@echo "Other Targets:"
	@echo "  make run             - Run agent (requires config.yaml)"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make help            - Show this help"
	@echo ""
	@echo "Version: $(VERSION)"
