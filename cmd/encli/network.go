// Client network layer — взаимодействие с сервером

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/encli-own/encli/pkg/crypto"
)

const (
	DefaultTimeout      = 30 * time.Second
	DefaultPollInterval = 10 * time.Second
)

// ClientNetwork — сетевой слой клиента.
type ClientNetwork struct {
	// Server address
	serverAddr string
	// HTTP client
	client *http.Client

	// Auth state
	sessionID string
	mailboxID string

	// Identity
	identity *crypto.Identity

	// Device public keys cache: deviceID -> pubkey
	deviceKeys map[string][]byte

	// Noise traffic enabled
	noiseEnabled bool
}

// NewClientNetwork создает новый сетевой клиент.
func NewClientNetwork() *ClientNetwork {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			// В production: добавить верификацию сертификата сервера
			InsecureSkipVerify: true, // ТОЛЬКО для разработки!
		},
	}

	return &ClientNetwork{
		client: &http.Client{
			Transport: tr,
			Timeout:   DefaultTimeout,
		},
		deviceKeys:   make(map[string][]byte),
		noiseEnabled: true,
	}
}

// Connect подключается к серверу и авторизуется.
func (cn *ClientNetwork) Connect(serverAddr string, identity *crypto.Identity) error {
	cn.serverAddr = serverAddr
	cn.identity = identity

	// Шаг 1: Получаем challenge от сервера
	challenge, err := cn.requestChallenge(identity.DeviceID)
	if err != nil {
		return fmt.Errorf("challenge request: %w", err)
	}

	// Шаг 2: Подписываем challenge
	sig, timestamp, err := identity.SignChallenge(challenge)
	if err != nil {
		return fmt.Errorf("signing challenge: %w", err)
	}

	// Шаг 3: Отправляем подпись и получаем токен
	token, err := cn.verifyChallenge(identity.DeviceID, hex.EncodeToString(sig), timestamp)
	if err != nil {
		return fmt.Errorf("challenge verification: %w", err)
	}

	cn.sessionID = token.SessionID
	cn.mailboxID = token.MailboxID

	return nil
}

// requestChallenge запрашивает challenge у сервера.
func (cn *ClientNetwork) requestChallenge(deviceID string) ([]byte, error) {
	url := fmt.Sprintf("https://%s/v1/auth/challenge?device_id=%s", cn.serverAddr, deviceID)

	resp, err := cn.client.Get(url)
	if err != nil {
		// Пробуем HTTP если HTTPS не работает
		url = fmt.Sprintf("http://%s/v1/auth/challenge?device_id=%s", cn.serverAddr, deviceID)
		resp, err = cn.client.Get(url)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Challenge string `json:"challenge"`
			TTL       int    `json:"ttl_seconds"`
		} `json:"data"`
		Error string `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("server error: %s", result.Error)
	}

	return hex.DecodeString(result.Data.Challenge)
}

// verifyChallenge отправляет подписанный challenge и получает токен.
func (cn *ClientNetwork) verifyChallenge(deviceID, signature string, timestamp int64) (*AuthTokenResponse, error) {
	url := fmt.Sprintf("https://%s/v1/auth/verify", cn.serverAddr)

	payload := map[string]interface{}{
		"device_id":  deviceID,
		"signature":  signature,
		"timestamp":  timestamp,
		"public_key": hex.EncodeToString(crypto.SerializeEd25519Public(cn.identity.Ed25519Public)),
	}

	payloadBytes, _ := json.Marshal(payload)

	resp, err := cn.client.Post(url, "application/json", bytes.NewReader(payloadBytes))
	if err != nil {
		url = fmt.Sprintf("http://%s/v1/auth/verify", cn.serverAddr)
		resp, err = cn.client.Post(url, "application/json", bytes.NewReader(payloadBytes))
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool              `json:"success"`
		Data    AuthTokenResponse `json:"data"`
		Error   string            `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("auth failed: %s", result.Error)
	}

	return &result.Data, nil
}

// PushMessage отправляет зашифрованное сообщение на сервер.
func (cn *ClientNetwork) PushMessage(mailboxID string, payload, encryptedHeader []byte) error {
	url := fmt.Sprintf("https://%s/v1/push", cn.serverAddr)

	reqBody := map[string]interface{}{
		"mailbox_id":       mailboxID,
		"payload":          hex.EncodeToString(payload),
		"encrypted_header": hex.EncodeToString(encryptedHeader),
	}

	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := cn.client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		url = fmt.Sprintf("http://%s/v1/push", cn.serverAddr)
		resp, err = cn.client.Post(url, "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// PullMessages извлекает сообщения из mailbox.
func (cn *ClientNetwork) PullMessages() ([]PulledMessage, error) {
	url := fmt.Sprintf("https://%s/v1/pull?mailbox_id=%s", cn.serverAddr, cn.mailboxID)

	resp, err := cn.client.Get(url)
	if err != nil {
		url = fmt.Sprintf("http://%s/v1/pull?mailbox_id=%s", cn.serverAddr, cn.mailboxID)
		resp, err = cn.client.Get(url)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull failed: %d", resp.StatusCode)
	}

	var result struct {
		Success bool         `json:"success"`
		Data    PullResponse `json:"data"`
		Error   string       `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("pull error: %s", result.Error)
	}

	return result.Data.Messages, nil
}

// SendNoise отправляет шумовой пакет (для обфускации трафика).
func (cn *ClientNetwork) SendNoise(noiseData []byte) error {
	if !cn.noiseEnabled {
		return nil
	}

	url := fmt.Sprintf("https://%s/v1/noise", cn.serverAddr)

	reqBody := map[string]string{
		"data": hex.EncodeToString(noiseData),
	}

	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := cn.client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		url = fmt.Sprintf("http://%s/v1/noise", cn.serverAddr)
		resp, err = cn.client.Post(url, "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			return err // Не критично — шум опционален
		}
	}
	defer resp.Body.Close()

	return nil
}

// GetServerManifest получает публичный манифест сервера.
func (cn *ClientNetwork) GetServerManifest(serverAddr string) (*ManifestResponse, error) {
	url := fmt.Sprintf("https://%s/v1/manifest", serverAddr)

	resp, err := cn.client.Get(url)
	if err != nil {
		url = fmt.Sprintf("http://%s/v1/manifest", serverAddr)
		resp, err = cn.client.Get(url)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result struct {
		Success bool             `json:"success"`
		Data    ManifestResponse `json:"data"`
		Error   string           `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}

// NoiseTrafficLoop запускает фоновую генерацию шумового трафика.
func (cn *ClientNetwork) NoiseTrafficLoop(ctx context.Context) {
	if !cn.noiseEnabled {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cn.noiseInterval()):
			// Генерируем и отправляем шум
			noise, _ := cn.generateNoisePacket()
			if noise != nil {
				cn.SendNoise(noise)
			}
		}
	}
}

// generateNoisePacket генерирует зашифрованный пакет-шум.
func (cn *ClientNetwork) generateNoisePacket() ([]byte, error) {
	// В реальной реализации: шифрование случайных данных
	// Здесь: просто случайные байты
	return crypto.SecureRandom(16384) // 16KB
}

// noiseInterval возвращает случайный интервал (30-90 секунд).
func (cn *ClientNetwork) noiseInterval() time.Duration {
	// В реальности: crypto.SecureRandom для интервала
	return 30 + time.Duration(rand.Int63n(int64(60)))*time.Second
}

// SendMessage отправляет сообщение (high-level API).
func (cn *ClientNetwork) SendMessage(msg Message) error {
	// В реальной реализации:
	// 1. Шифрование сообщения через Double Ratchet
	// 2. Padding до 16KB
	// 3. Push на сервер

	if cn.mailboxID == "" {
		return fmt.Errorf("not connected to server")
	}

	// Placeholder: отправка plaintext (для тестирования)
	payload := []byte(fmt.Sprintf("%s: %s", msg.Sender, msg.Content))

	return cn.PushMessage(cn.mailboxID, payload, nil)
}

// PulledMessage — структура для pull ответа (duplicate для клиента).
type PulledMessage struct {
	MessageID       string `json:"message_id"`
	Payload         string `json:"payload"`          // hex-encoded
	EncryptedHeader string `json:"encrypted_header"` // hex-encoded
	Timestamp       int64  `json:"timestamp"`
}

// PullResponse — структура ответа pull.
type PullResponse struct {
	Messages   []PulledMessage `json:"messages"`
	Count      int             `json:"count"`
	ServerTime int64           `json:"server_time"`
}

// AuthTokenResponse — ответ с токеном.
type AuthTokenResponse struct {
	SessionID string `json:"session_id"`
	MailboxID string `json:"mailbox_id"`
	ExpiresAt int64  `json:"expires_at"`
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
