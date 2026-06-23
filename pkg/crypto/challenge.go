package crypto

import (
	"crypto/sha256"
	"crypto/hmac"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/ed25519"
)

// ChallengeResponse реализует challenge-response авторизацию.
// Сервер не хранит пароли — только публичные ключи устройств.
type ChallengeResponse struct {
	// Challenge — случайная строка от сервера
	Challenge []byte
	// Timestamp — время создания challenge
	Timestamp time.Time
	// DeviceID — SHA-256(Ed25519_Public_Key) устройства
	DeviceID string
	// TTL — время жизни challenge
	TTL time.Duration
}

// DefaultChallengeTTL — время жизни challenge (5 минут).
const DefaultChallengeTTL = 5 * time.Minute

// GenerateChallenge создает новый challenge для устройства.
func GenerateChallenge(deviceID string) (*ChallengeResponse, error) {
	challenge, err := SecureRandom(32)
	if err != nil {
		return nil, fmt.Errorf("challenge generation failed: %w", err)
	}

	return &ChallengeResponse{
		Challenge: challenge,
		Timestamp: time.Now().UTC(),
		DeviceID:  deviceID,
		TTL:       DefaultChallengeTTL,
	}, nil
}

// SignChallenge подписывает challenge приватным ключом устройства.
// Возвращает подпись + timestamp для проверки свежести.
func (id *Identity) SignChallenge(challenge []byte) ([]byte, int64, error) {
	if len(challenge) == 0 {
		return nil, 0, fmt.Errorf("empty challenge")
	}
	// Подписываем challenge || timestamp
	timestamp := time.Now().UTC().Unix()
	data := append(challenge, int64Bytes(timestamp)...)
	sig := id.Sign(data)
	return sig, timestamp, nil
}

// VerifyChallengeResponse проверяет подпись challenge на сервере.
// pubKey — Ed25519 публичный ключ устройства (хранится на сервере).
func VerifyChallengeResponse(pubKey []byte, challenge []byte, sig []byte, timestamp int64, ttl time.Duration) error {
	// Проверяем свежесть
	challengeTime := time.Unix(timestamp, 0).UTC()
	if time.Since(challengeTime) > ttl {
		return fmt.Errorf("challenge expired")
	}

	// Восстанавливаем Ed25519 public key
	edPub, err := DeserializeEd25519Public(pubKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Верифицируем подпись
	data := append(challenge, int64Bytes(timestamp)...)
	if !VerifySignature(edPub, data, sig) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// int64Bytes конвертирует int64 в big-endian байты.
func int64Bytes(v int64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// ComputeDeviceID вычисляет DeviceID из Ed25519 публичного ключа.
func ComputeDeviceID(pubKey ed25519.PublicKey) string {
	hash := sha256.Sum256(pubKey)
	return hex.EncodeToString(hash[:])
}

// AuthToken — структура аутентификационного токена.
// Клиент получает его после успешного challenge-response.
type AuthToken struct {
	// SessionID — уникальный ID сессии
	SessionID string
	// DeviceID — идентификатор устройства
	DeviceID string
	// MailboxID — ID почтового ящика (SHA-256(deviceID || serverSalt))
	MailboxID string
	// ExpiresAt — время истечения токена
	ExpiresAt time.Time
	// HMAC подпись токена (серверным ключом)
	Signature []byte
}

// GenerateMailboxID вычисляет mailbox ID для устройства на сервере.
// Каждое устройство имеет изолированный mailbox.
func GenerateMailboxID(deviceID string, serverSalt []byte) string {
	h := sha256.New()
	h.Write([]byte(deviceID))
	h.Write(serverSalt)
	return hex.EncodeToString(h.Sum(nil))
}

// SessionManager управляет активными сессиями на сервере.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*AuthToken
	// serverSecret — секретный ключ сервера для подписи токенов
	serverSecret []byte
}

// NewSessionManager создает новый менеджер сессий.
func NewSessionManager() (*SessionManager, error) {
	secret, err := SecureRandom(32)
	if err != nil {
		return nil, err
	}
	return &SessionManager{
		sessions:     make(map[string]*AuthToken),
		serverSecret: secret,
	}, nil
}

// CreateSession создает новую сессию после успешной авторизации.
func (sm *SessionManager) CreateSession(deviceID string) (*AuthToken, error) {
	sessionID, err := SecureRandomHex(16)
	if err != nil {
		return nil, err
	}

	mailboxID := GenerateMailboxID(deviceID, sm.serverSecret)

	token := &AuthToken{
		SessionID: sessionID,
		DeviceID:  deviceID,
		MailboxID: mailboxID,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}

	// Подписываем токен
	token.Signature = HMAC(sm.serverSecret, []byte(token.SessionID+token.DeviceID+token.MailboxID))

	sm.mu.Lock()
	sm.sessions[token.SessionID] = token
	sm.mu.Unlock()

	return token, nil
}

// ValidateSession проверяет валидность сессии.
func (sm *SessionManager) ValidateSession(sessionID string) (*AuthToken, bool) {
	sm.mu.RLock()
	token, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().UTC().After(token.ExpiresAt) {
		// Сессия истекла
		sm.mu.Lock()
		delete(sm.sessions, sessionID)
		sm.mu.Unlock()
		return nil, false
	}

	// Проверяем подпись
	expectedSig := HMAC(sm.serverSecret, []byte(token.SessionID+token.DeviceID+token.MailboxID))
	if !hmac.Equal(expectedSig, token.Signature) {
		return nil, false
	}

	return token, true
}

// RevokeSession отзывает сессию.
func (sm *SessionManager) RevokeSession(sessionID string) {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()
}

// CleanupExpired удаляет истекшие сессии.
func (sm *SessionManager) CleanupExpired() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now().UTC()
	count := 0
	for id, token := range sm.sessions {
		if now.After(token.ExpiresAt) {
			delete(sm.sessions, id)
			count++
		}
	}
	return count
}
