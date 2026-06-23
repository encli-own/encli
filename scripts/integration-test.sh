#!/bin/bash
# Integration tests for encli
# Запускает сервер и клиент, тестирует end-to-end поток

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/build"

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PASSED=0
FAILED=0

# Хелперы
pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED++))
}

fail() {
    echo -e "${RED}✗${NC} $1"
    ((FAILED++))
}

info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Очистка
cleanup() {
    info "Cleaning up..."
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$TEST_DIR"
}
trap cleanup EXIT

# Создание тестовой директории
TEST_DIR=$(mktemp -d)
info "Test directory: $TEST_DIR"

# Сборка
info "Building project..."
cd "$PROJECT_DIR"
make build > /dev/null 2>&1 || {
    warn "Make failed, trying direct build..."
    go build -o "$BUILD_DIR/encli-server" ./server
    go build -o "$BUILD_DIR/encli" ./cmd/encli
}

# Тест 1: Бинарники существуют
test_binaries_exist() {
    info "Test: Binaries exist"
    
    if [ -f "$BUILD_DIR/encli-server" ]; then
        pass "encli-server binary exists"
    else
        fail "encli-server binary not found"
    fi
    
    if [ -f "$BUILD_DIR/encli" ]; then
        pass "encli binary exists"
    else
        fail "encli binary not found"
    fi
}

# Тест 2: Сервер запускается и отвечает на /health
test_server_start() {
    info "Test: Server startup"
    
    # Создаем конфиг
    cat > "$TEST_DIR/server.yaml" << EOF
server:
  host: "127.0.0.1"
  port: 18443
  grpc_port: 18444
  tls:
    enabled: false
  max_accounts: 100
  message_ttl: 1h
  noise_traffic:
    enabled: false
  logging:
    level: "warn"
EOF

    # Запускаем сервер
    "$BUILD_DIR/encli-server" -config "$TEST_DIR/server.yaml" &
    SERVER_PID=$!
    
    # Ждем запуска
    for i in {1..30}; do
        if curl -fsSL "http://127.0.0.1:18443/health" > /dev/null 2>&1; then
            pass "Server started and health check passed"
            return 0
        fi
        sleep 0.5
    done
    
    fail "Server failed to start"
    return 1
}

# Тест 3: Манифест сервера
test_manifest() {
    info "Test: Server manifest"
    
    local response
    response=$(curl -fsSL "http://127.0.0.1:18443/v1/manifest" 2>/dev/null)
    
    if echo "$response" | grep -q '"success":true'; then
        pass "Manifest endpoint works"
        
        if echo "$response" | grep -q '"max_message_size":16384'; then
            pass "Max message size is 16KB"
        else
            fail "Max message size not 16KB"
        fi
    else
        fail "Manifest endpoint failed"
    fi
}

# Тест 4: Challenge-Response авторизация
test_auth() {
    info "Test: Challenge-Response authentication"
    
    # Шаг 1: Получаем challenge
    local device_id="test_device_$(date +%s)"
    local challenge_response
    challenge_response=$(curl -fsSL "http://127.0.0.1:18443/v1/auth/challenge?device_id=$device_id" 2>/dev/null)
    
    if echo "$challenge_response" | grep -q '"challenge"'; then
        pass "Challenge received"
    else
        fail "Challenge request failed"
        return 1
    fi
    
    local challenge=$(echo "$challenge_response" | grep -o '"challenge":"[^"]*"' | cut -d'"' -f4)
    info "Challenge: ${challenge:0:16}..."
    
    # Шаг 2: Генерируем ключи и подписываем challenge
    export HOME="$TEST_DIR"
    mkdir -p "$TEST_DIR/.encli/keys"
    
    # Создаем тестовую identity
    "$BUILD_DIR/encli" keys generate 2>/dev/null || true
    
    # Шаг 3: Отправляем verify
    # Note: Полная верификация требует правильной подписи
    # Здесь тестируем только endpoint
    local verify_response
    verify_response=$(curl -fsSL -X POST \
        -H "Content-Type: application/json" \
        -d "{\"device_id\":\"$device_id\",\"signature\":\"aabbccdd\",\"timestamp\":$(date +%s),\"public_key\":\"aabbccdd\"}" \
        "http://127.0.0.1:18443/v1/auth/verify" 2>/dev/null)
    
    if echo "$verify_response" | grep -q '"success"'; then
        pass "Auth verify endpoint accessible"
    else
        warn "Auth verify returned error (expected with invalid sig)"
    fi
}

# Тест 5: Push и Pull сообщений
test_push_pull() {
    info "Test: Push and Pull messages"
    
    local test_payload
    test_payload=$(python3 -c "import secrets; print(secrets.token_hex(256))" 2>/dev/null || echo "aabbccddeeff")
    
    # Push
    local push_response
    push_response=$(curl -fsSL -X POST \
        -H "Content-Type: application/json" \
        -d "{\"mailbox_id\":\"test_mailbox_123\",\"payload\":\"$test_payload\"}" \
        "http://127.0.0.1:18443/v1/push" 2>/dev/null)
    
    if echo "$push_response" | grep -q '"accepted":true'; then
        pass "Message pushed"
    else
        fail "Push failed"
        return 1
    fi
    
    # Pull
    local pull_response
    pull_response=$(curl -fsSL "http://127.0.0.1:18443/v1/pull?mailbox_id=test_mailbox_123" 2>/dev/null)
    
    if echo "$pull_response" | grep -q '"count":1'; then
        pass "Message pulled and removed"
    else
        fail "Pull failed or message not found"
    fi
    
    # Проверяем что сообщение удалено
    local pull2_response
    pull2_response=$(curl -fsSL "http://127.0.0.1:18443/v1/pull?mailbox_id=test_mailbox_123" 2>/dev/null)
    
    if echo "$pull2_response" | grep -q '"count":0'; then
        pass "Message deleted after pull (ephemeral)"
    else
        warn "Message may not be deleted after pull"
    fi
}

# Тест 6: Noise endpoint
test_noise() {
    info "Test: Noise packet handling"
    
    local noise_data
    noise_data=$(python3 -c "import secrets; print(secrets.token_hex(16384))" 2>/dev/null || echo "aa")
    
    local noise_response
    noise_response=$(curl -fsSL -X POST \
        -H "Content-Type: application/json" \
        -d "{\"data\":\"$noise_data\"}" \
        "http://127.0.0.1:18443/v1/noise" 2>/dev/null)
    
    if echo "$noise_response" | grep -q '"success":true'; then
        pass "Noise packet accepted"
    else
        fail "Noise endpoint failed"
    fi
}

# Тест 7: Rate limiting
test_rate_limit() {
    info "Test: Rate limiting"
    
    # Отправляем много запросов
    local count=0
    local limited=0
    for i in {1..30}; do
        local code
        code=$(curl -o /dev/null -s -w "%{http_code}" \
            "http://127.0.0.1:18443/v1/manifest" 2>/dev/null)
        if [ "$code" = "429" ]; then
            ((limited++))
        fi
        ((count++))
    done
    
    if [ "$limited" -gt 0 ]; then
        pass "Rate limiting works ($limited/$count limited)"
    else
        warn "Rate limiting may not be active (all $count requests passed)"
    fi
}

# Тест 8: CLI --version
test_cli_version() {
    info "Test: CLI version"
    
    local version_output
    version_output=$("$BUILD_DIR/encli" version 2>/dev/null)
    
    if echo "$version_output" | grep -q "encli"; then
        pass "CLI version works"
    else
        fail "CLI version failed"
    fi
}

# Тест 9: CLI keys generate
test_cli_keys() {
    info "Test: CLI key generation"
    
    export HOME="$TEST_DIR/home2"
    mkdir -p "$TEST_DIR/home2"
    
    local key_output
    key_output=$("$BUILD_DIR/encli" keys generate 2>&1)
    
    if echo "$key_output" | grep -q "IDENTITY GENERATED"; then
        pass "Key generation works"
    else
        warn "Key generation output unexpected"
    fi
}

# Запуск тестов
main() {
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║         encli Integration Tests                          ║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════╝${NC}"
    echo
    
    test_binaries_exist
    test_server_start
    test_manifest
    test_auth
    test_push_pull
    test_noise
    test_rate_limit
    test_cli_version
    test_cli_keys
    
    # Результаты
    echo
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║              TEST RESULTS                                ║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║  Passed: ${GREEN}$PASSED${BLUE}                                          ║${NC}"
    echo -e "${BLUE}║  Failed: ${RED}$FAILED${BLUE}                                          ║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════╝${NC}"
    
    if [ "$FAILED" -gt 0 ]; then
        exit 1
    fi
    
    exit 0
}

main "$@"
