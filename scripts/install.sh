#!/bin/bash
# encli installer script
# Устанавливает encli клиент и/или сервер

set -e

REPO="github.com/encli-own/encli"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.encli"

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Определение архитектуры
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l|armhf)
            echo "arm"
            ;;
        i386|i686)
            echo "386"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# Определение ОС
detect_os() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case $os in
        linux)
            echo "linux"
            ;;
        darwin)
            echo "darwin"
            ;;
        mingw*|msys*|cygwin*)
            echo "windows"
            ;;
        *)
            log_error "Unsupported OS: $os"
            exit 1
            ;;
    esac
}

# Проверка зависимостей
check_dependencies() {
    local deps=("curl" "tar")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Required dependency not found: $dep"
            exit 1
        fi
    done
}

# Скачивание бинарников
download_binaries() {
    local os=$1
    local arch=$2
    local version=${3:-"latest"}
    
    log_info "Downloading encli for $os-$arch (version: $version)..."
    
    local tmpdir=$(mktemp -d)
    local base_url="https://github.com/encli-own/encli/releases/download"
    
    if [ "$version" = "latest" ]; then
        base_url="https://github.com/encli-own/encli/releases/latest/download"
    fi
    
    # Скачиваем архив
    local archive="encli-${version}-${os}-${arch}.tar.gz"
    local url="${base_url}/${archive}"
    
    log_info "Downloading from $url..."
    
    if ! curl -fsSL -o "$tmpdir/$archive" "$url" 2>/dev/null; then
        log_error "Download failed. Trying alternative URL..."
        # Пробуем сборку из исходников
        build_from_source
        return
    fi
    
    # Распаковка
    log_info "Extracting archive..."
    tar -xzf "$tmpdir/$archive" -C "$tmpdir"
    
    # Установка
    log_info "Installing binaries..."
    if [ "$INSTALL_MODE" = "server" ] || [ "$INSTALL_MODE" = "both" ]; then
        sudo cp "$tmpdir/encli-server" "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/encli-server"
        log_success "encli-server installed to $INSTALL_DIR"
    fi
    
    if [ "$INSTALL_MODE" = "client" ] || [ "$INSTALL_MODE" = "both" ]; then
        sudo cp "$tmpdir/encli" "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/encli"
        log_success "encli client installed to $INSTALL_DIR"
    fi
    
    # Очистка
    rm -rf "$tmpdir"
}

# Сборка из исходников
build_from_source() {
    log_info "Building from source..."
    
    if ! command -v go &> /dev/null; then
        log_error "Go is required for building from source"
        log_info "Install Go: https://golang.org/doc/install"
        exit 1
    fi
    
    local go_version=$(go version | awk '{print $3}' | sed 's/go//')
    log_info "Go version: $go_version"
    
    # Клонирование
    local tmpdir=$(mktemp -d)
    log_info "Cloning repository..."
    git clone --depth 1 "https://$REPO.git" "$tmpdir/encli"
    cd "$tmpdir/encli"
    
    # Сборка
    log_info "Building..."
    make deps
    make build
    
    # Установка
    if [ "$INSTALL_MODE" = "server" ] || [ "$INSTALL_MODE" = "both" ]; then
        sudo cp "build/encli-server" "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/encli-server"
        log_success "encli-server installed"
    fi
    
    if [ "$INSTALL_MODE" = "client" ] || [ "$INSTALL_MODE" = "both" ]; then
        sudo cp "build/encli" "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/encli"
        log_success "encli client installed"
    fi
    
    # Очистка
    rm -rf "$tmpdir"
}

# Создание конфигурации
setup_config() {
    log_info "Setting up configuration..."
    
    mkdir -p "$CONFIG_DIR/keys"
    chmod 700 "$CONFIG_DIR"
    
    if [ ! -f "$CONFIG_DIR/client.yaml" ]; then
        cat > "$CONFIG_DIR/client.yaml" << 'EOF'
client:
  server:
    address: "localhost:8443"
    tls: true
  crypto:
    keys_path: "~/.encli/keys"
  ui:
    theme: "dark"
    timestamps: true
  storage:
    ephemeral: true
  network:
    poll_interval: 10s
    noise_traffic: true
EOF
        log_success "Client config created at $CONFIG_DIR/client.yaml"
    fi
    
    if [ "$INSTALL_MODE" = "server" ] || [ "$INSTALL_MODE" = "both" ]; then
        if [ ! -f "$CONFIG_DIR/server.yaml" ]; then
            sudo mkdir -p /etc/encli
            sudo cp "configs/server.yaml" /etc/encli/server.yaml 2>/dev/null || true
            log_success "Server config created at /etc/encli/server.yaml"
        fi
    fi
}

# Установка systemd сервиса
install_systemd_service() {
    if [ "$INSTALL_MODE" != "server" ] && [ "$INSTALL_MODE" != "both" ]; then
        return
    fi
    
    if ! command -v systemctl &> /dev/null; then
        log_warn "systemd not found, skipping service installation"
        return
    fi
    
    log_info "Installing systemd service..."
    
    sudo tee /etc/systemd/system/encli-server.service > /dev/null << 'EOF'
[Unit]
Description=encli Blind Relay Server
After=network.target

[Service]
Type=simple
User=encli
Group=encli
ExecStart=/usr/local/bin/encli-server -config /etc/encli/server.yaml
Restart=always
RestartSec=5
Environment="ENCLI_LOG_LEVEL=info"

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable encli-server
    
    log_success "Systemd service installed"
    log_info "Start server: sudo systemctl start encli-server"
    log_info "View logs: sudo journalctl -u encli-server -f"
}

# Генерация ключей
generate_keys() {
    if [ "$INSTALL_MODE" = "server" ]; then
        return
    fi
    
    log_info "Generating cryptographic keys..."
    
    if [ ! -f "$CONFIG_DIR/keys/identity.enc" ]; then
        encli keys generate || {
            log_warn "Key generation skipped (manual required)"
            return
        }
    else
        log_warn "Keys already exist, skipping generation"
    fi
}

# Вывод помощи
show_help() {
    cat << 'EOF'
encli installer

Usage: ./install.sh [OPTIONS] [MODE]

Modes:
    client          Install client only
    server          Install server only
    both            Install both (default)

Options:
    -v, --version   Specify version (default: latest)
    -d, --dir       Installation directory (default: /usr/local/bin)
    -h, --help      Show this help
    --source        Build from source

Examples:
    ./install.sh                    # Install both client and server
    ./install.sh client             # Install client only
    ./install.sh server             # Install server only
    ./install.sh --version v1.0.0   # Install specific version
    ./install.sh --source           # Build from source

EOF
}

# Главная функция
main() {
    echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║        encli installer                   ║${NC}"
    echo -e "${GREEN}║   Encrypted CLI Messenger                ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
    echo
    
    # Парсинг аргументов
    INSTALL_MODE="both"
    VERSION="latest"
    FROM_SOURCE=false
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -v|--version)
                VERSION="$2"
                shift 2
                ;;
            -d|--dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            --source)
                FROM_SOURCE=true
                shift
                ;;
            client|server|both)
                INSTALL_MODE="$1"
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    log_info "Installation mode: $INSTALL_MODE"
    log_info "Install directory: $INSTALL_DIR"
    
    # Проверка зависимостей
    check_dependencies
    
    # Определение платформы
    OS=$(detect_os)
    ARCH=$(detect_arch)
    log_info "Platform: $OS-$ARCH"
    
    # Скачивание или сборка
    if [ "$FROM_SOURCE" = true ]; then
        build_from_source
    else
        download_binaries "$OS" "$ARCH" "$VERSION"
    fi
    
    # Конфигурация
    setup_config
    
    # Systemd сервис
    install_systemd_service
    
    # Генерация ключей
    generate_keys
    
    # Финальный вывод
    echo
    echo -e "${GREEN}╔══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║           INSTALLATION COMPLETE                          ║${NC}"
    echo -e "${GREEN}╠══════════════════════════════════════════════════════════╣${NC}"
    
    if [ "$INSTALL_MODE" = "client" ] || [ "$INSTALL_MODE" = "both" ]; then
        echo -e "${GREEN}║  Client: encli                                          ║${NC}"
        echo -e "${GREEN}║  Config: $CONFIG_DIR${NC}"
        echo -e "${GREEN}║  Keys:   $CONFIG_DIR/keys${NC}"
        echo -e "${GREEN}║                                                         ║${NC}"
        echo -e "${GREEN}║  Quick start:                                           ║${NC}"
        echo -e "${GREEN}║    encli keys generate                                  ║${NC}"
        echo -e "${GREEN}║    encli register <server:port>                         ║${NC}"
        echo -e "${GREEN}║    encli                                                ║${NC}"
        echo -e "${GREEN}║                                                         ║${NC}"
    fi
    
    if [ "$INSTALL_MODE" = "server" ] || [ "$INSTALL_MODE" = "both" ]; then
        echo -e "${GREEN}║  Server: encli-server                                   ║${NC}"
        echo -e "${GREEN}║  Config: /etc/encli/server.yaml                         ║${NC}"
        echo -e "${GREEN}║                                                         ║${NC}"
        echo -e "${GREEN}║  Quick start:                                           ║${NC}"
        echo -e "${GREEN}║    sudo systemctl start encli-server                    ║${NC}"
        echo -e "${GREEN}║    sudo journalctl -u encli-server -f                   ║${NC}"
        echo -e "${GREEN}║                                                         ║${NC}"
    fi
    
    echo -e "${GREEN}╚══════════════════════════════════════════════════════════╝${NC}"
    echo
    log_info "For more info: encli --help"
    log_info "Documentation: https://github.com/encli-own/encli#readme"
}

# Запуск
main "$@"
