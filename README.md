# encli — Encrypted CLI Messenger

> **Zero-Knowledge. End-to-End Encrypted. Metadata-Resistant. Federated.**

`encli` (encrypted CLI) — это федеративный децентрализованный консольный мессенджер с архитектурой нулевого разглашения (Zero-Knowledge) и встроенной защитой от анализа метаданных.

---

## Содержание

- [Философия](#философия)
- [Особенности](#особенности)
- [Архитектура](#архитектура)
- [Установка](#установка)
- [Быстрый старт](#быстрый-старт)
- [Использование](#использование)
- [Конфигурация](#конфигурация)
- [API сервера](#api-сервера)
- [Развертывание сервера](#развертывание-сервера)
- [Криптография](#криптография)
- [Безопасность](#безопасность)
- [Разработка](#разработка)
- [Роадмап](#роадмап)
- [Лицензия](#лицензия)

---

## Философия

encli создан по принципу **"доверяй, но проверяй"** — криптографически:

- **Сервер не знает ничего** — ни содержимого сообщений, ни никнеймов, ни связей между пользователями. Сервер — это "слепой транзитный шлюз" (Blind Relay).
- **Никаких паролей** — авторизация исключительно через криптографические ключи Ed25519 (Challenge-Response).
- **Никаких телефонов/почты** — никакой привязки к реальному миру. Только криптографические идентификаторы.
- **Метаданные защищены** — фиксированный размер пакетов (16KB), шумовой трафик, отсутствие временных корреляций.
- **Полная кроссплатформенность** — работает на Windows, macOS, Linux и Android (через Termux).

---

## Особенности

### Криптография
| Алгоритм | Назначение |
|----------|-----------|
| **Ed25519** | Подпись и авторизация (Challenge-Response) |
| **X25519** | Обмен ключами Диффи-Хеллмана |
| **AES-256-GCM** | Симметричное E2EE шифрование |
| **Double Ratchet** | Forward secrecy + Future secrecy |
| **SHA-256** | Хеширование (Device ID, Mailbox ID) |

### Защита метаданных
- **Фиксированный пакет** — все сообщения padded до 16KB
- **Шумовой трафик** — dummy packets каждые 30-90 секунд
- **Blind relay** — сервер не знает получателей
- **Mailbox system** — каждое устройство имеет изолированный ящик
- **No logging** — сервер не логирует метаданные

### Архитектура
- **Федеративная** — любой может поднять свой сервер
- **Zero-knowledge** — сервер не может прочитать сообщения даже теоретически
- **Мультидевайс** — синхронизация через отдельные ключи на устройства
- **Эфемерный кэш** — сообщения хранятся только в ОЗУ сервера (TTL = 7 дней)
- **Self-destruct** — клиентский кэш уничтожается при выходе (Shredding)

---

## Архитектура

```
┌─────────────┐     E2EE (Double Ratchet)      ┌─────────────┐
│   Клиент A   │◄──────────────────────────────►│   Клиент B   │
│  (TUI/CLI)   │                                │  (TUI/CLI)   │
└──────┬───────┘                                └──────┬───────┘
       │                                              │
       │         Blind Relay Server                   │
       │         (Zero Knowledge)                     │
       │                                              │
       ▼                                              ▼
┌─────────────────────────────────────────────────────────┐
│  HTTPS/gRPC              ┌──────────────┐               │
│  Fixed 16KB packets  ───►│   Mailbox A  │ (isolated)   │
│  Noise traffic       ───►│   Mailbox B  │ (isolated)   │
│                         │   Mailbox C  │ (isolated)   │
│                         └──────────────┘               │
│  In-Memory Only  ◄──  TTL 7 days  ◄──  Pull = Delete │
└─────────────────────────────────────────────────────────┘
```

### Поток авторизации (Challenge-Response)
```
1. Клиент ──GET /v1/auth/challenge?device_id=<hash>──► Сервер
2. Клиент ◄──────────challenge (32 random bytes)───────── Сервер
3. Клиент ──POST /v1/auth/verify {sig(challenge)}──────► Сервер
4. Клиент ◄──────────{session_id, mailbox_id}──────────── Сервер
```

### Поток сообщения
```
1. Клиент шифрует сообщение (Double Ratchet)
2. Padding до 16KB
3. POST /v1/push {mailbox_id, payload} ──► Сервер ──► Mailbox
4. Получатель: GET /v1/pull?mailbox_id=<> ◄── Сервер ◄── Mailbox
5. Сообщения удаляются с сервера после pull
```

---

## Установка

### Требования
- **Go 1.21+** (для сборки)
- **SQLite3** (для SQLCipher хранилища)
- **Make** (опционально)
- **Docker** (для контейнерного развертывания)

### Из исходников
```bash
# Клонирование
git clone https://github.com/encli-own/encli.git
cd encli

# Установка зависимостей
make deps

# Сборка
make build

# Или: сборка под все платформы
make build-all
```

### Бинарники
Готовые бинарники для всех платформ доступны в [Releases](https://github.com/encli-own/encli/releases).

### Docker
```bash
docker-compose -f deployments/docker/docker-compose.yml up -d
```

### Android (Termux)
```bash
pkg install golang git

git clone https://github.com/encli-own/encli.git
cd encli

GOOS=android GOARCH=arm64 go build -o encli ./cmd/encli
chmod +x encli
./encli keys generate
```

---

## Быстрый старт

### 1. Генерация ключей (один раз)
```bash
# Клиент
./encli keys generate

# Результат: создается Device ID и Fingerprint
# Ключи сохраняются в ~/.encli/keys/
```

### 2. Регистрация на сервере
```bash
./encli register localhost:8443
```

### 3. Запуск TUI
```bash
./encli
```

Интерфейс:
```
 encli  — Encrypted CLI Messenger
╔══════════════════════════════════════════════════════════╗
║ Conversations                                           ║
╠══════════════════════════════════════════════════════════╣
║ > Welcome              Welcome to encli!               ║
║   Alice                [1] Hey, how are you?           ║
║   Bob                  See you tomorrow                 ║
║   Work Group           Meeting at 3pm                   ║
╚══════════════════════════════════════════════════════════╝
 [q quit] [n new chat] [s settings] [esc back]
```

Управление:
- `↑/↓` — навигация по чатам
- `Enter` — открыть чат
- `n` — новый чат
- `Ctrl+S` — отправить сообщение
- `s` — настройки
- `q` / `Ctrl+C` — выход

### 4. Отправка сообщения (CLI)
```bash
./encli send <device-id> "Hello, this is secret!"
```

---

## Использование

### CLI команды

```bash
# Показать версию
encli version

# Генерация ключей
encli keys generate

# Показать публичные ключи
encli keys show

# Регистрация на сервере
encli register <server:port>

# Отправить сообщение
encli send <recipient-device-id> "message text"

# Информация о сервере
encli info <server:port>

# Запуск TUI (интерактивный режим)
encli
encli --server localhost:8443
```

### TUI горячие клавиши

| Клавиша | Действие |
|---------|----------|
| `↑/↓` или `k/j` | Навигация по чатам |
| `Enter` | Открыть выбранный чат |
| `n` | Новый чат |
| `s` | Настройки |
| `Ctrl+S` | Отправить сообщение |
| `Esc` | Назад / Отмена |
| `q` / `Ctrl+C` | Выход |

### Добавление нового устройства
```bash
# На основном устройстве:
encli device add
# Показывается QR-код

# На новом устройстве:
encli device scan
# Сканировать QR-код
```

---

## Конфигурация

### Сервер (`configs/server.yaml`)
```yaml
server:
  host: "0.0.0.0"
  port: 8443
  grpc_port: 8444
  
  tls:
    enabled: true
    cert_path: "/path/to/cert.pem"
    key_path: "/path/to/key.pem"
    auto_cert: false
  
  max_accounts: 10000
  max_devices_per_account: 10
  max_message_size: 16384  # 16 KB
  max_mailbox_size: 100
  message_ttl: 168h        # 7 дней
  
  noise_traffic:
    enabled: true
    min_interval: 30s
    max_interval: 90s
  
  federation:
    enabled: true
    trusted_nodes_file: "trusted_nodes.json"
    sync_interval: 5m
  
  logging:
    level: "info"          # debug, info, warn, error
    format: "json"
    output: "stdout"
```

### Переменные окружения сервера
| Переменная | Описание | По умолчанию |
|------------|----------|-------------|
| `ENCLI_SERVER_HOST` | Bind address | `0.0.0.0` |
| `ENCLI_SERVER_PORT` | HTTP порт | `8443` |
| `ENCLI_GRPC_PORT` | gRPC порт | `8444` |
| `ENCLI_MESSAGE_TTL` | TTL сообщений | `168h` |
| `ENCLI_MAX_ACCOUNTS` | Макс. аккаунтов | `10000` |
| `ENCLI_LOG_LEVEL` | Уровень логов | `info` |

### Клиент (`configs/client.yaml`)
```yaml
client:
  server:
    address: "localhost:8443"
    tls: true
  
  crypto:
    keys_path: "~/.encli/keys"
  
  ui:
    theme: "dark"
    sidebar_width: 30
    timestamps: true
  
  storage:
    ephemeral: true  # Уничтожение данных при выходе
  
  network:
    poll_interval: 10s
    noise_traffic: true
```

---

## API сервера

### Публичные endpoints (без авторизации)

#### GET `/health` — Health check
```bash
curl http://localhost:8443/health
```

#### GET `/v1/manifest` — Публичный манифест
```bash
curl http://localhost:8443/v1/manifest
# Ответ: {server_id, version, max_accounts, message_ttl, ...}
```

### Авторизация

#### GET `/v1/auth/challenge?device_id=<hex>`
Получить challenge для авторизации.

**Ответ:**
```json
{
  "success": true,
  "data": {
    "challenge": "a1b2c3d4...",
    "ttl_seconds": 300
  }
}
```

#### POST `/v1/auth/verify`
Отправить подписанный challenge.

**Тело:**
```json
{
  "device_id": "sha256_hash...",
  "signature": "ed25519_sig...",
  "timestamp": 1700000000,
  "public_key": "ed25519_pubkey..."
}
```

**Ответ:**
```json
{
  "success": true,
  "data": {
    "session_id": "random_session...",
    "mailbox_id": "mailbox_hash...",
    "expires_at": 1700000000
  }
}
```

### Сообщения

#### POST `/v1/push` — Отправить сообщение
```bash
curl -X POST http://localhost:8443/v1/push \
  -H "Content-Type: application/json" \
  -d '{
    "mailbox_id": "recipient_mailbox...",
    "payload": "hex_encoded_encrypted_payload...",
    "encrypted_header": "hex_encoded_header..."
  }'
```

#### GET `/v1/pull?mailbox_id=<>` — Получить сообщения
```bash
curl "http://localhost:8443/v1/pull?mailbox_id=your_mailbox..."
# Сообщения удаляются после получения!
```

#### POST `/v1/noise` — Шумовой пакет (no-op)
```bash
curl -X POST http://localhost:8443/v1/noise \
  -d '{"data": "random_16kb_hex..."}'
```

### Администрирование

#### GET `/v1/stats` — Статистика сервера (требует API key)
```bash
curl -H "X-API-Key: your_api_key" http://localhost:8443/v1/stats
```

---

## Развертывание сервера

### Docker (рекомендуется)
```bash
cd deployments/docker

# Запуск
docker-compose up -d

# Просмотр логов
docker-compose logs -f encli-server

# Остановка
docker-compose down
```

### Системный сервис (systemd)
```bash
# Копируем бинарник
sudo cp build/encli-server /usr/local/bin/
sudo chmod +x /usr/local/bin/encli-server

# Создаем пользователя
sudo useradd -r -s /bin/false encli

# Создаем конфиг
sudo mkdir -p /etc/encli
sudo cp configs/server.yaml /etc/encli/
sudo chown -R encli:encli /etc/encli

# Системный сервис
sudo tee /etc/systemd/system/encli-server.service << 'EOF'
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

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable encli-server
sudo systemctl start encli-server
```

### Бинарник (ручной запуск)
```bash
# Запуск с конфигом
./encli-server -config configs/server.yaml

# Переменные окружения
ENCLI_LOG_LEVEL=debug ENCLI_SERVER_PORT=8443 ./encli-server
```

---

## Криптография

### Ключевая схема
```
┌──────────────────────────────────────────────────────────┐
│                     КЛИЕНТ                              │
│  ┌─────────────────┐        ┌──────────────────────┐    │
│  │   Ed25519       │        │   X25519              │    │
│  │   (Signing)     │        │   (Key Exchange)      │    │
│  │                 │        │                       │    │
│  │  DeviceID =     │        │  SharedSecret =       │    │
│  │  SHA-256(Pub)   │        │  X25519(PrivA, PubB)  │    │
│  └────────┬────────┘        └──────────┬────────────┘    │
│           │                            │                 │
│           ▼                            ▼                 │
│  ┌──────────────────────────────────────────┐           │
│  │        Double Ratchet Session            │           │
│  │  RootKey ──► ChainKey ──► MessageKey    │           │
│  │         KDF_RK    KDF_CK                 │           │
│  └──────────────────────────────────────────┘           │
└──────────────────────────────────────────────────────────┘
```

### Double Ratchet
- **DH Ratchet** — меняет ключи каждые N сообщений (forward secrecy)
- **Symmetric Ratchet** — цепочка KDF для каждого сообщения (future secrecy)
- **Skip keys** — обработка пропущенных/переупорядоченных сообщений

### Формат пакета
```
+-----------+----------------+---------------------+------------------+----------+
|  Version  |   MessageID    |     MailboxID       |    Payload       |  Padding |
|  (1 byte) |   (16 bytes)   |     (32 bytes)      |   (variable)     | (to 16KB)|
+-----------+----------------+---------------------+------------------+----------+
```

---

## Безопасность

### Что сервер знает
- **НЕ ЗНАЕТ** содержимого сообщений
- **НЕ ЗНАЕТ** никнеймов пользователей
- **НЕ ЗНАЕТ** кто с кем общается
- **НЕ ЗНАЕТ** реальный размер сообщений
- **ЗНАЕТ ТОЛЬКО** — mailbox ID (случайный хеш) и зашифрованный opaque blob

### Что сервер хранит
- **В ОЗУ только** — зашифрованные сообщения до pull
- **Ничего на диске** — in-memory хранилище (опционально: encrypted persistence)
- **Сообщения удаляются** сразу после /v1/pull
- **TTL = 7 дней** — автоудаление незабранных сообщений

### Защита клиента
- **SQLCipher** — локальная БД с шифрованием
- **Эфемерный режим** — все данные уничтожаются при выходе (Shredding)
- **Мастер-пароль** — защита приватных ключей
- **Нулевая сессия** — ничего не сохраняется между запусками (опционально)

### Threat model
| Угроза | Защита |
|--------|--------|
| Пассивный перехват | E2EE + Fixed padding |
| Активный MITM | Ed25519 signatures + Certificate pinning |
| Компрометация сервера | Zero-knowledge архитектура |
| Компрометация ключа | Double Ratchet (forward secrecy) |
| Анализ метаданных | Noise traffic + Fixed packet size |
| Forensic на клиенте | Ephemeral mode + Secure wipe |

---

## Разработка

### Структура проекта
```
encli/
├── cmd/encli/          # TUI клиент
│   ├── main.go         # Точка входа, CLI команды
│   ├── tui.go          # Bubbletea TUI
│   ├── network.go      # HTTP/gRPC клиент
│   └── identity.go     # Управление ключами
├── server/             # Blind relay сервер
│   ├── main.go         # Точка входа
│   ├── config.go       # Конфигурация
│   ├── server.go       # HTTP/gRPC сервер
│   ├── handlers.go     # API handlers
│   ├── storage.go      # In-memory хранилище
│   └── ratelimit.go    # Rate limiting
├── pkg/
│   ├── crypto/         # Криптографическое ядро
│   │   ├── keys.go     # Ed25519/X25519
│   │   ├── cipher.go   # AES-256-GCM + Padding
│   │   ├── ratchet.go  # Double Ratchet
│   │   └── challenge.go # Challenge-Response auth
│   ├── protocol/       # Форматы сообщений
│   │   └── message.go  # Envelope, InnerMessage
│   └── transport/      # Сетевые абстракции
├── configs/            # Конфигурации
├── deployments/        # Docker, Kubernetes
├── scripts/            # Вспомогательные скрипты
└── docs/               # Документация
```

### Сборка
```bash
# Сборка всего
make build

# Сборка только сервера
make build-server

# Сборка только клиента
make build-client

# Кросс-компиляция (Linux, macOS, Windows, Android)
make build-all

# Тесты
make test

# Docker
make docker
```

### Тестирование
```bash
# Unit tests
make test

# Тесты криптографии
make test-crypto

# Тесты сервера
make test-server

# Интеграционные тесты
./scripts/integration-test.sh
```

---

## Роадмап

### v1.0 — MVP (Current)
- [x] Базовая криптография (Ed25519, X25519, AES-256-GCM)
- [x] Blind relay сервер
- [x] Challenge-Response авторизация
- [x] TUI клиент на Bubbletea
- [x] Фиксированные 16KB пакеты
- [x] Шумовой трафик
- [x] Docker развертывание
- [x] Эфемерный режим

### v1.1 — Federation
- [ ] gRPC федерация между серверами
- [ ] Trusted nodes registry
- [] Сервер-сквозная маршрутизация

### v1.2 — File Transfer
- [ ] Шифрованная передача файлов
- [] Chunked upload/download
- [] File integrity (SHA-256)

### v1.3 — Voice/Video
- [ ] WebRTC signaling
- [] Голосовые звонки
- [] Видеозвонки

### v1.4 — Mobile
- [ ] Нативное Termux IPC
- [] Push notifications (Termux)
- [] Background daemon

### v2.0 — Hardening
- [ ] Постквантовая криптография (Kyber/Dilithium)
- [ ] Reproducible builds
- [ ] Formal verification (crypto)

---

## Лицензия

**AGPL-3.0** — Копилефт, все производные работы должны быть открыты.

```
encli — Encrypted CLI Messenger
Copyright (C) 2024 encli contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.

See the GNU Affero General Public License for more details.
```

---

## Контакты

- **GitHub**: https://github.com/encli-own/encli
- **Matrix**: #encli:matrix.org
- **Email**: dev@encli.github.io

**Безопасность**: security@encli.github.io (PGP: 0x...)

---

> **"Privacy is not a privilege. It's a right."** — encli
