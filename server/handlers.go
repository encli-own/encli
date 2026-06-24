package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/encli-own/encli/pkg/crypto"
)

// APIResponse — стандартный ответ API.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// AuthChallengeResponse — ответ с challenge для авторизации.
type AuthChallengeResponse struct {
	Challenge string `json:"challenge"`   // hex-encoded challenge
	TTL       int    `json:"ttl_seconds"` // время жизни в секундах
}

// AuthTokenResponse — ответ с токеном после авторизации.
type AuthTokenResponse struct {
	SessionID string `json:"session_id"`
	MailboxID string `json:"mailbox_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// PushRequest — запрос на отправку сообщения.
type PushRequest struct {
	// MailboxID получателя (клиент знает это из регистрации или key exchange)
	MailboxID string `json:"mailbox_id"`
	// Зашифрованный payload (opaque для сервера)
	Payload string `json:"payload"` // hex-encoded
	// Зашифрованный заголовок Double Ratchet
	EncryptedHeader string `json:"encrypted_header,omitempty"` // hex-encoded
}

// PushResponse — ответ на push.
type PushResponse struct {
	MessageID string `json:"message_id"`
	Accepted  bool   `json:"accepted"`
	QueueSize int    `json:"queue_size"`
}

// PullResponse — ответ с сообщениями.
type PullResponse struct {
	Messages   []PulledMessage `json:"messages"`
	Count      int             `json:"count"`
	ServerTime int64           `json:"server_time"`
}

// PulledMessage — сообщение из mailbox.
type PulledMessage struct {
	MessageID       string `json:"message_id"`
	Payload         string `json:"payload"`          // hex-encoded
	EncryptedHeader string `json:"encrypted_header"` // hex-encoded
	Timestamp       int64  `json:"timestamp"`
}

// ManifestResponse — публичный манифест сервера.
type ManifestResponse struct {
	ServerID        string `json:"server_id"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
	Operator        string `json:"operator,omitempty"`
	Description     string `json:"description"`
	Region          string `json:"region,omitempty"`
	MaxAccounts     int    `json:"max_accounts"`
	CurrentAccounts int    `json:"current_accounts"`
	MessageTTL      int    `json:"message_ttl_hours"`
	MaxMessageSize  int    `json:"max_message_size"`
	NoiseSupport    bool   `json:"noise_support"`
	Federation      bool   `json:"federation"`
}

// NoisePacket — структура шумового пакета.
type NoisePacket struct {
	// Opaque данные — сервер не анализирует содержимое
	Data string `json:"data"` // hex-encoded random data
}

// Error codes
const (
	ErrCodeInvalidRequest  = "INVALID_REQUEST"
	ErrCodeUnauthorized    = "UNAUTHORIZED"
	ErrCodeMailboxNotFound = "MAILBOX_NOT_FOUND"
	ErrCodeMailboxFull     = "MAILBOX_FULL"
	ErrCodeRateLimited     = "RATE_LIMITED"
	ErrCodeServerFull      = "SERVER_FULL"
	ErrCodeInternalError   = "INTERNAL_ERROR"
)

// writeJSON пишет JSON-ответ.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError пишет ошибку.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, APIResponse{
		Success: false,
		Error:   fmt.Sprintf("[%s] %s", code, message),
	})
}

// --- HTTP Handlers ---

// handleManifest возвращает публичный манифест сервера.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	stats := s.storage.Stats()

	manifest := ManifestResponse{
		ServerID:        s.config.Server.Manifest.ServerID,
		Version:         s.config.Server.Manifest.Version,
		ProtocolVersion: 1,
		Operator:        s.config.Server.Manifest.Operator,
		Description:     s.config.Server.Manifest.Description,
		Region:          s.config.Server.Manifest.Region,
		MaxAccounts:     s.config.Server.MaxAccounts,
		CurrentAccounts: stats.TotalAccounts,
		MessageTTL:      int(s.config.Server.MessageTTL.Hours()),
		MaxMessageSize:  s.config.Server.MaxMessageSize,
		NoiseSupport:    s.config.Server.NoiseTraffic.Enabled,
		Federation:      s.config.Server.Federation.Enabled,
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    manifest,
	})
}

// handleAuthChallenge — шаг 1: сервер выдает challenge.
// GET /v1/auth/challenge?device_id=<hex>
func (s *Server) handleAuthChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "device_id required")
		return
	}

	// Валидация формата deviceID (hex string)
	if _, err := hex.DecodeString(deviceID); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid device_id format")
		return
	}

	// Генерируем challenge
	challenge, err := crypto.SecureRandom(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "challenge generation failed")
		return
	}

	// Сохраняем challenge во временном хранилище
	s.challengesMu.Lock()
	s.challenges[deviceID] = &pendingChallenge{
		Challenge: challenge,
		CreatedAt: time.Now().UTC(),
		DeviceID:  deviceID,
	}
	s.challengesMu.Unlock()

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: AuthChallengeResponse{
			Challenge: hex.EncodeToString(challenge),
			TTL:       int(crypto.DefaultChallengeTTL.Seconds()),
		},
	})
}

// handleAuthVerify — шаг 2: проверка подписи и выдача токена.
// POST /v1/auth/verify
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "reading body")
		return
	}

	var req struct {
		DeviceID  string `json:"device_id"`
		Signature string `json:"signature"` // hex
		Timestamp int64  `json:"timestamp"`
		PublicKey string `json:"public_key"` // hex Ed25519 pubkey
	}

	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid JSON")
		return
	}

	// Проверяем challenge
	s.challengesMu.Lock()
	pending, ok := s.challenges[req.DeviceID]
	if !ok {
		s.challengesMu.Unlock()
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "challenge not found or expired")
		return
	}
	delete(s.challenges, req.DeviceID)
	s.challengesMu.Unlock()

	// Проверяем TTL challenge
	if time.Since(pending.CreatedAt) > crypto.DefaultChallengeTTL {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "challenge expired")
		return
	}

	// Верифицируем подпись
	sig, err := hex.DecodeString(req.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid signature format")
		return
	}

	pubKeyBytes, err := hex.DecodeString(req.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid public key format")
		return
	}

	if err := crypto.VerifyChallengeResponse(pubKeyBytes, pending.Challenge, sig, req.Timestamp, crypto.DefaultChallengeTTL); err != nil {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, fmt.Sprintf("signature verification failed: %v", err))
		return
	}

	// Создаем сессию
	token, err := s.sessionManager.CreateSession(req.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "session creation failed")
		return
	}

	// Регистрируем mailbox
	mailboxID, err := s.storage.RegisterDevice(req.DeviceID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeServerFull, err.Error())
		return
	}

	// Сохраняем публичный ключ устройства (для будущих верификаций)
	s.keysMu.Lock()
	s.deviceKeys[req.DeviceID] = pubKeyBytes
	s.keysMu.Unlock()

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: AuthTokenResponse{
			SessionID: token.SessionID,
			MailboxID: mailboxID,
			ExpiresAt: token.ExpiresAt.Unix(),
		},
	})
}

// handlePush — отправка зашифрованного сообщения в mailbox.
// POST /v1/push
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST allowed")
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "reading body")
		return
	}

	var req PushRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid JSON")
		return
	}

	// Декодируем payload
	payload, err := hex.DecodeString(req.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid payload encoding")
		return
	}

	// Проверяем размер (должен быть ровно 16KB с padding)
	if len(payload) > s.config.Server.MaxMessageSize {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest,
			fmt.Sprintf("payload too large: %d > %d", len(payload), s.config.Server.MaxMessageSize))
		return
	}

	var encryptedHeader []byte
	if req.EncryptedHeader != "" {
		encryptedHeader, err = hex.DecodeString(req.EncryptedHeader)
		if err != nil {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid header encoding")
			return
		}
	}

	// Генерируем message ID
	msgIDBytes := sha256.Sum256(append(payload, []byte(fmt.Sprintf("%d", time.Now().UnixNano()))...))
	msgID := hex.EncodeToString(msgIDBytes[:16])

	msg := &Message{
		MessageID:       msgID,
		Payload:         payload,
		EncryptedHeader: encryptedHeader,
		TTL:             s.config.Server.MessageTTL,
		OriginalSize:    len(payload),
	}

	// Сохраняем в mailbox
	if err := s.storage.StoreMessage(req.MailboxID, msg); err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "storage error")
		return
	}

	// Получаем текущий размер очереди
	mb, _ := s.storage.GetMailbox(req.MailboxID)
	queueSize := 0
	if mb != nil {
		queueSize = mb.Count()
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: PushResponse{
			MessageID: msgID,
			Accepted:  true,
			QueueSize: queueSize,
		},
	})
}

// handlePull — извлечение сообщений из mailbox (с удалением).
// GET /v1/pull?mailbox_id=<>&session_id=<>
func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	mailboxID := r.URL.Query().Get("mailbox_id")
	if mailboxID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "mailbox_id required")
		return
	}

	// Rate limiting
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded")
		return
	}

	// Извлекаем и удаляем сообщения
	messages, err := s.storage.RetrieveMessages(mailboxID)
	if err != nil {
		writeError(w, http.StatusNotFound, ErrCodeMailboxNotFound, "mailbox not found")
		return
	}

	resp := PullResponse{
		Messages:   make([]PulledMessage, 0, len(messages)),
		Count:      len(messages),
		ServerTime: time.Now().UTC().Unix(),
	}

	for _, msg := range messages {
		resp.Messages = append(resp.Messages, PulledMessage{
			MessageID:       msg.MessageID,
			Payload:         hex.EncodeToString(msg.Payload),
			EncryptedHeader: hex.EncodeToString(msg.EncryptedHeader),
			Timestamp:       msg.CreatedAt.Unix(),
		})

		// Secure wipe после сериализации
		secureWipe(msg.Payload)
		secureWipe(msg.EncryptedHeader)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    resp,
	})
}

// handlePeek — просмотр сообщений без удаления.
// GET /v1/peek?mailbox_id=<>
func (s *Server) handlePeek(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	mailboxID := r.URL.Query().Get("mailbox_id")
	if mailboxID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "mailbox_id required")
		return
	}

	mb, ok := s.storage.GetMailbox(mailboxID)
	if !ok {
		writeError(w, http.StatusNotFound, ErrCodeMailboxNotFound, "mailbox not found")
		return
	}

	messages := mb.Peek()
	resp := PullResponse{
		Messages:   make([]PulledMessage, 0, len(messages)),
		Count:      len(messages),
		ServerTime: time.Now().UTC().Unix(),
	}

	for _, msg := range messages {
		resp.Messages = append(resp.Messages, PulledMessage{
			MessageID:       msg.MessageID,
			Payload:         hex.EncodeToString(msg.Payload),
			EncryptedHeader: hex.EncodeToString(msg.EncryptedHeader),
			Timestamp:       msg.CreatedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    resp,
	})
}

// handleNoise — прием шумового пакета (no-op, для обфускации).
// POST /v1/noise
func (s *Server) handleNoise(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST allowed")
		return
	}

	// Читаем тело (16KB шум)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "reading body")
		return
	}

	// Проверяем размер (должен быть ~16KB)
	if len(body) < 1024 {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "noise packet too small")
		return
	}

	// No-op — сервер не анализирует шумовые пакеты
	// В будущем: можно логировать для статистики (без содержимого)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"status": "ack"},
	})
}

// handleStats — статистика сервера (только для администраторов).
// GET /v1/stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	// Проверка API ключа (простая)
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" || apiKey != s.apiKey {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid API key")
		return
	}

	stats := s.storage.Stats()
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    stats,
	})
}

// handleHealth — health check.
// GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Unix(),
		},
	})
}

// --- WebSocket Handlers (для real-time) ---

// handleWebSocket — WebSocket соединение для real-time сообщений.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Проверяем, что WebSocket включен
	if !s.config.Server.Federation.Enabled {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "WebSocket not enabled")
		return
	}

	// Upgrade до WebSocket
	// В реальной реализации: github.com/gorilla/websocket
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "WebSocket upgrade not implemented in this version")
}

// --- Directory Handlers ---

// handleDirectoryPublish публикует зашифрованный профиль в директорию.
// POST /v1/directory/publish
// Body: {"nickname":"alice","encrypted_profile":"hex..."}
// Сервер хранит только blinded_id → encrypted_profile, не зная nickname.
func (s *Server) handleDirectoryPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST allowed")
		return
	}

	var req struct {
		Nickname string `json:"nickname"`
		Profile  string `json:"encrypted_profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid JSON")
		return
	}

	if req.Nickname == "" || req.Profile == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "nickname and encrypted_profile required")
		return
	}

	entry, err := s.directory.Publish(req.Nickname, req.Profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"blinded_id": entry.BlindedID,
			"timestamp":  entry.Timestamp,
		},
	})
}

// handleDirectoryLookup ищет профиль по blinded_id (точное совпадение).
// GET /v1/directory/lookup?blinded_id=<hex>
func (s *Server) handleDirectoryLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	blindedID := r.URL.Query().Get("blinded_id")
	if blindedID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "blinded_id required")
		return
	}

	entry := s.directory.Lookup(blindedID)
	if entry == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "profile not found")
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"profile":   entry.Profile,
			"timestamp": entry.Timestamp,
		},
	})
}

// handleDirectorySearch ищет профили по префиксу blinded_id.
// GET /v1/directory/search?q=<hex_prefix>
func (s *Server) handleDirectorySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET allowed")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "search query required")
		return
	}

	results := s.directory.Search(query)

	type resultItem struct {
		BlindedID string `json:"blinded_id"`
		Profile   string `json:"profile"`
		Timestamp int64  `json:"timestamp"`
	}

	items := make([]resultItem, 0, len(results))
	for _, entry := range results {
		items = append(items, resultItem{
			BlindedID: entry.BlindedID,
			Profile:   entry.Profile,
			Timestamp: entry.Timestamp,
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"results": items,
			"count":   len(items),
		},
	})
}

// --- Helper functions ---

// getClientIP возвращает IP клиента.
func getClientIP(r *http.Request) string {
	// Проверяем X-Forwarded-For
	fwd := r.Header.Get("X-Forwarded-For")
	if fwd != "" {
		return fwd
	}
	return r.RemoteAddr
}

// pendingChallenge — ожидающий challenge.
type pendingChallenge struct {
	Challenge []byte
	CreatedAt time.Time
	DeviceID  string
}

// generateAPIKey генерирует случайный API ключ для /stats.
func generateAPIKey() string {
	key, _ := crypto.SecureRandomHex(32)
	return key
}

// noisePacketInterval возвращает случайный интервал для шумового трафика.
func noisePacketInterval(minInterval, maxInterval time.Duration) time.Duration {
	if minInterval >= maxInterval {
		return minInterval
	}
	delta := maxInterval - minInterval
	return minInterval + time.Duration(rand.Int63n(int64(delta)))
}
