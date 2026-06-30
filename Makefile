# ===========================================
# Makefile - starcat-discovery-api
# ===========================================
VERSION := 0.1.0

BINARY_NAME := server
BIN_DIR := bin
MAIN_PATH := ./cmd/server
PKG := ./...

GO := go
GOFMT := gofmt -s -l
GOVET := go vet
GOBUILD := $(GO) build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)
GOTEST := $(GO) test -v -race -coverprofile=coverage.out $(PKG)
GOCLEAN := $(GO) clean
GOMOD := $(GO) mod

.DEFAULT_GOAL := help

.PHONY: help
help: ## 显示帮助信息
	@echo "可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## 运行服务 (开发模式)
	@$(GO) run $(MAIN_PATH)

.PHONY: build
build: ## 编译二进制到 bin/server
	@mkdir -p $(BIN_DIR)
	@$(GOBUILD)
	@echo "✓ 编译完成: $(BIN_DIR)/$(BINARY_NAME)"

.PHONY: clean
clean: ## 清理构建产物
	@$(GOCLEAN)
	@rm -rf $(BIN_DIR)
	@rm -f coverage.out coverage.html
	@echo "✓ 清理完成"

.PHONY: fmt
fmt: ## 格式化代码
	@$(GO) fmt $(PKG)
	@echo "✓ 格式化完成"

.PHONY: fmt-check
fmt-check: ## 检查代码格式 (CI 用)
	@if [ "$$($(GOFMT) $(PKG) | wc -l)" -gt 0 ]; then \
		echo "✗ 以下文件未格式化:"; \
		$(GOFMT) $(PKG); \
		exit 1; \
	fi
	@echo "✓ 格式检查通过"

.PHONY: vet
vet: ## 静态分析
	@$(GOVET) $(PKG)
	@echo "✓ vet 通过"

.PHONY: test
test: ## 运行单元测试
	@$(GOTEST)
	@echo "✓ 测试通过"

.PHONY: deps
deps: ## 下载依赖
	@$(GOMOD) download
	@$(GOMOD) verify
	@echo "✓ 依赖已就绪"

.PHONY: tidy
tidy: ## 整理 go.mod / go.sum
	@$(GOMOD) tidy
	@echo "✓ go.mod / go.sum 已更新"

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像
	@docker build -t starcat-discovery-api:latest .
	@echo "✓ Docker 镜像构建完成: starcat-discovery-api:latest"

.PHONY: docker-run
docker-run: ## 运行 Docker 容器
	@docker run --rm -p 5006:5006 starcat-discovery-api:latest

.PHONY: check
check: fmt-check vet test ## 完整检查 (format + vet + test)
	@echo "✓ 全部检查通过"

.PHONY: all
all: clean deps check build ## 完整构建
	@echo "✓ 完整构建完成"

release: check build ## 发布版本
	@./scripts/deploy.sh v$(VERSION)
