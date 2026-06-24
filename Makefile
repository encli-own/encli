# encli — Encrypted CLI Messenger
# Makefile для сборки сервера и клиента

.PHONY: all build build-server build-client build-all clean test proto docker run-server run-client install lint fmt help

# Переменные
BINARY_NAME_SERVER=encli-server
BINARY_NAME_CLIENT=encli
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

GO=go
GOFLAGS=-trimpath

# Директории
SERVER_DIR=./server
CLIENT_DIR=./cmd/encli
BUILD_DIR=./build

# Цвета для вывода
BLUE=\033[36m
GREEN=\033[32m
YELLOW=\033[33m
RED=\033[31m
NC=\033[0m

## all: Сборка всего проекта
all: build

## build: Сборка сервера и клиента для текущей платформы
build: build-server build-client
	@echo "$(GREEN)✓ Сборка завершена$(NC)"
	@echo "  Сервер: $(BUILD_DIR)/$(BINARY_NAME_SERVER)"
	@echo "  Клиент: $(BUILD_DIR)/$(BINARY_NAME_CLIENT)"

## build-server: Сборка сервера
build-server:
	@echo "$(BLUE)→ Сборка сервера...$(NC)"
	@mkdir -p $(BUILD_DIR)
	cd $(SERVER_DIR) && $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME_SERVER) .
	@echo "$(GREEN)✓ Сервер собран$(NC)"

## build-client: Сборка TUI-клиента
build-client:
	@echo "$(BLUE)→ Сборка клиента...$(NC)"
	@mkdir -p $(BUILD_DIR)
	cd $(CLIENT_DIR) && $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/$(BINARY_NAME_CLIENT) .
	@echo "$(GREEN)✓ Клиент собран$(NC)"

## build-all: Кросс-компиляция под все платформы
build-all: build-linux build-darwin build-windows build-android

build-linux:
	@echo "$(BLUE)→ Сборка для Linux (amd64)...$(NC)"
	@mkdir -p $(BUILD_DIR)/linux-amd64
	cd $(SERVER_DIR) && GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/linux-amd64/$(BINARY_NAME_SERVER) .
	cd $(CLIENT_DIR) && GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/linux-amd64/$(BINARY_NAME_CLIENT) .
	@echo "$(GREEN)✓ Linux amd64$(NC)"

build-linux-arm64:
	@echo "$(BLUE)→ Сборка для Linux (arm64)...$(NC)"
	@mkdir -p $(BUILD_DIR)/linux-arm64
	cd $(SERVER_DIR) && GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/linux-arm64/$(BINARY_NAME_SERVER) .
	cd $(CLIENT_DIR) && GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/linux-arm64/$(BINARY_NAME_CLIENT) .
	@echo "$(GREEN)✓ Linux arm64$(NC)"

build-darwin:
	@echo "$(BLUE)→ Сборка для macOS...$(NC)"
	@mkdir -p $(BUILD_DIR)/darwin-amd64 $(BUILD_DIR)/darwin-arm64
	cd $(SERVER_DIR) && GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/darwin-amd64/$(BINARY_NAME_SERVER) .
	cd $(CLIENT_DIR) && GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/darwin-amd64/$(BINARY_NAME_CLIENT) .
	cd $(SERVER_DIR) && GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/darwin-arm64/$(BINARY_NAME_SERVER) .
	cd $(CLIENT_DIR) && GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/darwin-arm64/$(BINARY_NAME_CLIENT) .
	@echo "$(GREEN)✓ macOS amd64 + arm64$(NC)"

build-windows:
	@echo "$(BLUE)→ Сборка для Windows...$(NC)"
	@mkdir -p $(BUILD_DIR)/windows-amd64
	cd $(SERVER_DIR) && GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/windows-amd64/$(BINARY_NAME_SERVER).exe .
	cd $(CLIENT_DIR) && GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/windows-amd64/$(BINARY_NAME_CLIENT).exe .
	@echo "$(GREEN)✓ Windows amd64$(NC)"

build-android:
	@echo "$(BLUE)→ Сборка для Android (Termux)...$(NC)"
	@mkdir -p $(BUILD_DIR)/android-arm64
	cd $(SERVER_DIR) && GOOS=android GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/android-arm64/$(BINARY_NAME_SERVER) .
	cd $(CLIENT_DIR) && GOOS=android GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o ../../$(BUILD_DIR)/android-arm64/$(BINARY_NAME_CLIENT) .
	@echo "$(GREEN)✓ Android arm64$(NC)"

## proto: Генерация protobuf файлов
proto:
	@echo "$(BLUE)→ Генерация protobuf...$(NC)"
	@mkdir -p pkg/protocol
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		pkg/protocol/encli.proto
	@echo "$(GREEN)✓ Protobuf сгенерирован$(NC)"

## test: Запуск тестов
test:
	@echo "$(BLUE)→ Запуск тестов...$(NC)"
	$(GO) test -v -race ./pkg/...
	@echo "$(GREEN)✓ Тесты пройдены$(NC)"

## test-crypto: Тесты криптографии
test-crypto:
	@echo "$(BLUE)→ Тесты криптографии...$(NC)"
	$(GO) test -v ./pkg/crypto/...

## test-server: Тесты сервера
test-server:
	@echo "$(BLUE)→ Тесты сервера...$(NC)"
	$(GO) test -v ./server/...

## docker: Сборка Docker образов
docker:
	@echo "$(BLUE)→ Сборка Docker образов...$(NC)"
	docker build -t encli-server:latest -f deployments/docker/Dockerfile.server .
	docker build -t encli-client:latest -f deployments/docker/Dockerfile.client .
	@echo "$(GREEN)✓ Docker образы собраны$(NC)"

## docker-run: Запуск сервера в Docker
docker-run:
	@echo "$(BLUE)→ Запуск сервера в Docker...$(NC)"
	docker compose -f deployments/docker/docker-compose.yml up -d
	@echo "$(GREEN)✓ Сервер запущен$(NC)"

## docker-stop: Остановка Docker контейнеров
docker-stop:
	@echo "$(YELLOW)→ Остановка контейнеров...$(NC)"
	docker compose -f deployments/docker/docker-compose.yml down
	@echo "$(GREEN)✓ Контейнеры остановлены$(NC)"

## run-server: Запуск сервера локально (для разработки)
run-server: build-server
	@echo "$(BLUE)→ Запуск сервера...$(NC)"
	$(BUILD_DIR)/$(BINARY_NAME_SERVER) -config configs/server.yaml

## run-client: Запуск клиента
run-client: build-client
	@echo "$(BLUE)→ Запуск клиента...$(NC)"
	$(BUILD_DIR)/$(BINARY_NAME_CLIENT)

## fmt: Форматирование кода
fmt:
	@echo "$(BLUE)→ Форматирование...$(NC)"
	gofmt -w -s .
	goimports -w .
	@echo "$(GREEN)✓ Форматирование завершено$(NC)"

## lint: Линтинг
lint:
	@echo "$(BLUE)→ Линтинг...$(NC)"
	golangci-lint run ./...
	@echo "$(GREEN)✓ Линтинг завершен$(NC)"

## deps: Установка зависимостей
deps:
	@echo "$(BLUE)→ Установка зависимостей...$(NC)"
	$(GO) mod download
	$(GO) mod tidy
	@echo "$(GREEN)✓ Зависимости установлены$(NC)"

## clean: Очистка build артефактов
clean:
	@echo "$(YELLOW)→ Очистка...$(NC)"
	rm -rf $(BUILD_DIR)/
	$(GO) clean -cache
	@echo "$(GREEN)✓ Очистка завершена$(NC)"

## install: Установка бинарников в GOPATH/bin
install: build
	@echo "$(BLUE)→ Установка...$(NC)"
	cp $(BUILD_DIR)/$(BINARY_NAME_SERVER) $(GOPATH)/bin/ 2>/dev/null || cp $(BUILD_DIR)/$(BINARY_NAME_SERVER) $(HOME)/go/bin/ 2>/dev/null || echo "$(YELLOW)⚠ Не удалось установить сервер$(NC)"
	cp $(BUILD_DIR)/$(BINARY_NAME_CLIENT) $(GOPATH)/bin/ 2>/dev/null || cp $(BUILD_DIR)/$(BINARY_NAME_CLIENT) $(HOME)/go/bin/ 2>/dev/null || echo "$(YELLOW)⚠ Не удалось установить клиент$(NC)"
	@echo "$(GREEN)✓ Установка завершена$(NC)"

## help: Показать эту справку
help:
	@echo "$(GREEN)encli — Encrypted CLI Messenger$(NC)"
	@echo "$(GREEN)================================$(NC)"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'

.DEFAULT_GOAL := help
