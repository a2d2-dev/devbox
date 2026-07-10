.PHONY: build run clean test test-unit test-coverage deps build-linux build-arm help ui build-all

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)

BINARY := bin/devbox

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

all: build

deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

build: deps
	@echo "Building A2D2 Devbox (version: $(VERSION))..."
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags "$(LDFLAGS)" \
		-o $(BINARY) \
		./cmd/devbox

# ui: 前端构建 + 同步到 pkg/console/dist（Go embed 读的是这里，
# 不是 console-ui/dist；漏这一步二进制里嵌的还是旧 bundle）
ui:
	@echo "Building console UI..."
	cd console-ui && npm run build
	@echo "Syncing dist -> pkg/console/dist ..."
	rm -rf pkg/console/dist
	cp -r console-ui/dist pkg/console/dist

# build-all: 一步到位，前端 + 后端，避免忘同步 dist 导致跑旧 bundle
build-all: ui build

run: build
	@echo "Running A2D2 Devbox..."
	@if [ ! -f config.yaml ]; then \
		echo "Error: config.yaml not found. Copy config.yaml.example and modify it."; \
		exit 1; \
	fi
	$(BINARY) -config config.yaml

clean:
	@echo "Cleaning..."
	rm -rf bin/
	go clean

test: test-unit

test-unit:
	@echo "Running unit tests..."
	go test -v -race -cover -tags=unit ./pkg/... ./cmd/...

test-coverage:
	@echo "Generating coverage report..."
	go test -v -race -cover -coverprofile=coverage.out -tags=unit ./pkg/... ./cmd/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo ""
	@echo "Overall coverage:"
	@go tool cover -func=coverage.out | tail -1

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 $(MAKE) build

build-arm:
	@echo "Building for ARM64..."
	GOOS=linux GOARCH=arm64 $(MAKE) build

help:
	@echo "A2D2 Devbox Makefile"
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
	@echo "  make test-coverage   - Generate coverage report"
	@echo ""
	@echo "Other Targets:"
	@echo "  make run             - Run devbox (requires config.yaml)"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make help            - Show this help"
	@echo ""
	@echo "Version: $(VERSION)"
