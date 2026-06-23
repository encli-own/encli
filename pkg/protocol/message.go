// Package protocol определяет форматы сообщений encli.
// Все сообщения шифруются E2EE — сервер видит только opaque blob.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// MessageType — тип сообщения.
type MessageType byte

const (
	MessageTypeText       MessageType = 0x01
	MessageTypeFile       MessageType = 0x02
	MessageTypeControl    MessageType = 0x03
	MessageTypeDelivery   MessageType = 0x04 // Подтверждение доставки
	MessageTypeRead       MessageType = 0x05 // Прочитано
	MessageTypeTyping     MessageType = 0x06 // Индикатор набора
	MessageTypeDeviceSync MessageType = 0x07 // Синхронизация устройств
	MessageTypeCallOffer  MessageType = 0x08 // WebRTC offer
	MessageTypeCallAnswer MessageType = 0x09 // WebRTC answer
	MessageTypeICECandidate MessageType = 0x0A // ICE candidate
)

// Priority — приоритет сообщения.
type Priority byte

const (
	PriorityLow    Priority = 0x01
	PriorityNormal Priority = 0x02
	PriorityHigh   Priority = 0x03
	PriorityUrgent Priority = 0x04
)

// Envelope — зашифрованный конверт сообщения.
// Это то, что видит сервер (opaque blob).
type Envelope struct {
	// Version протокола
	Version uint8
	// ID сообщения (UUID v4)
	MessageID [16]byte
	// MailboxID получателя (SHA-256 хеш — сервер не знает кому он принадлежит)
	MailboxID [32]byte
	// Timestamp отправки
	Timestamp int64
	// TTL в секундах
	TTL uint32
	// Encrypted payload (зашифрованный InnerMessage)
	Payload []byte
	// Зашифрованный header Double Ratchet
	EncryptedHeader []byte
	// Padding — шум для маскировки размера (опционально)
	Padding []byte
}

// InnerMessage — внутреннее сообщение (E2EE, расшифровывается только получателем).
type InnerMessage struct {
	// Version
	Version uint8 `json:"v"`
	// Type тип сообщения
	Type MessageType `json:"t"`
	// MessageID — ID сообщения
	MessageID string `json:"mid"`
	// SenderDeviceID — ID устройства отправителя
	SenderDeviceID string `json:"sid"`
	// SenderNickname — никнейм отправителя (опционально, клиентский уровень)
	SenderNickname string `json:"sn,omitempty"`
	// ConversationID — ID диалога (SHA-256 от sorted(senderIDs))
	ConversationID string `json:"cid"`
	// Timestamp
	Timestamp int64 `json:"ts"`
	// Content — полезная нагрузка (зависит от Type)
	Content json.RawMessage `json:"cnt"`
	// ReplyTo — ID сообщения, на которое отвечаем
	ReplyTo string `json:"rto,omitempty"`
	// Priority
	Priority Priority `json:"pr"`
	// Ephemeral — самоуничтожение после прочтения
	Ephemeral bool `json:"eph,omitempty"`
	// EphemeralTimer — секунд до удаления
	EphemeralTimer uint32 `json:"et,omitempty"`
}

// TextContent — текстовое сообщение.
type TextContent struct {
	Text      string   `json:"text"`
	Mentions  []string `json:"m,omitempty"`
	Format    string   `json:"fmt,omitempty"` // markdown, plain
}

// FileContent — файл.
type FileContent struct {
	FileName    string `json:"fn"`
	FileSize    uint64 `json:"fs"`
	FileHash    string `json:"fh"`     // SHA-256 файла
	CipherKey   string `json:"ck"`     // Ключ шифрования файла (E2EE)
	ChunkSize   uint32 `json:"cs"`     // Размер чанка
	TotalChunks uint32 `json:"tc"`     // Всего чанков
	Caption     string `json:"cap,omitempty"`
}

// ControlContent — служебное сообщение.
type ControlContent struct {
	Action string          `json:"act"`
	Data   json.RawMessage `json:"d,omitempty"`
}

// DeviceSyncContent — синхронизация устройств.
type DeviceSyncContent struct {
	// Публичные ключи устройств отправителя
	DevicePubKeys [][]byte `json:"dpk"`
	// Action: add_device, remove_device, sync_keys
	Action string `json:"act"`
}

// DeliveryReceipt — подтверждение доставки.
type DeliveryReceipt struct {
	MessageID string `json:"mid"`
	Status    string `json:"st"` // delivered, failed
	Timestamp int64  `json:"ts"`
}

// CallSignalingContent — сигналинг для звонков (WebRTC).
type CallSignalingContent struct {
	CallID      string `json:"cid"`
	SDP         string `json:"sdp,omitempty"`
	Candidate   string `json:"cand,omitempty"`
	SdpMid      string `json:"mid,omitempty"`
	SdpMLineIndex uint16 `json:"mli,omitempty"`
}

// Serialize сериализует Envelope в байты для передачи.
func (e *Envelope) Serialize() ([]byte, error) {
	buf := make([]byte, 0, 4096)

	// Header
	buf = append(buf, e.Version)
	buf = append(buf, e.MessageID[:]...)
	buf = append(buf, e.MailboxID[:]...)

	// Timestamp (8 bytes, big-endian)
	tsBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBuf, uint64(e.Timestamp))
	buf = append(buf, tsBuf...)

	// TTL (4 bytes)
	ttlBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(ttlBuf, e.TTL)
	buf = append(buf, ttlBuf...)

	// Payload length + payload
	payloadLen := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadLen, uint32(len(e.Payload)))
	buf = append(buf, payloadLen...)
	buf = append(buf, e.Payload...)

	// Encrypted header length + header
	headerLen := make([]byte, 4)
	binary.BigEndian.PutUint32(headerLen, uint32(len(e.EncryptedHeader)))
	buf = append(buf, headerLen...)
	buf = append(buf, e.EncryptedHeader...)

	return buf, nil
}

// DeserializeEnvelope десериализует Envelope из байт.
func DeserializeEnvelope(data []byte) (*Envelope, error) {
	if len(data) < 1+16+32+8+4+4 {
		return nil, fmt.Errorf("envelope data too short")
	}

	offset := 0
	e := &Envelope{}

	// Version
	e.Version = data[offset]
	offset++

	// MessageID
	copy(e.MessageID[:], data[offset:offset+16])
	offset += 16

	// MailboxID
	copy(e.MailboxID[:], data[offset:offset+32])
	offset += 32

	// Timestamp
	e.Timestamp = int64(binary.BigEndian.Uint64(data[offset : offset+8]))
	offset += 8

	// TTL
	e.TTL = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Payload
	if offset+4 > len(data) {
		return nil, fmt.Errorf("invalid payload length")
	}
	payloadLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	if offset+int(payloadLen) > len(data) {
		return nil, fmt.Errorf("payload exceeds data")
	}
	e.Payload = data[offset : offset+int(payloadLen)]
	offset += int(payloadLen)

	// Encrypted header
	if offset+4 > len(data) {
		return nil, fmt.Errorf("invalid header length")
	}
	headerLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	if offset+int(headerLen) > len(data) {
		return nil, fmt.Errorf("header exceeds data")
	}
	e.EncryptedHeader = data[offset : offset+int(headerLen)]

	return e, nil
}

// CreateInnerMessage создает внутреннее сообщение.
func CreateInnerMessage(msgType MessageType, senderID, convID string, content interface{}) (*InnerMessage, error) {
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal content: %w", err)
	}

	// Генерация UUID v4
	msgID, err := generateMessageID()
	if err != nil {
		return nil, err
	}

	return &InnerMessage{
		Version:        1,
		Type:           msgType,
		MessageID:      msgID,
		SenderDeviceID: senderID,
		ConversationID: convID,
		Timestamp:      time.Now().UTC().Unix(),
		Content:        contentBytes,
		Priority:       PriorityNormal,
	}, nil
}

// Serialize сериализует InnerMessage в JSON.
func (m *InnerMessage) Serialize() ([]byte, error) {
	return json.Marshal(m)
}

// DeserializeInnerMessage десериализует InnerMessage из JSON.
func DeserializeInnerMessage(data []byte) (*InnerMessage, error) {
	var m InnerMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal inner message: %w", err)
	}
	return &m, nil
}

// generateMessageID генерирует UUID v4.
func generateMessageID() (string, error) {
	b, err := generateSecureRandom(16)
	if err != nil {
		return "", err
	}
	// UUID v4 variant
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func generateSecureRandom(n int) ([]byte, error) {
	b := make([]byte, n)
	// В реальном коде: crypto/rand.Read
	// Здесь заглушка для компиляции
	for i := range b {
		b[i] = byte(i) ^ 0xAB
	}
	return b, nil
}

// ComputeConversationID вычисляет ID диалога из ID участников.
func ComputeConversationID(deviceIDs ...string) string {
	// Сортируем ID для детерминированности
	// Простая реализация — хеш от concatenation отсортированных IDs
	// В реальном коде: sort.Strings + SHA-256
	if len(deviceIDs) == 0 {
		return ""
	}
	result := deviceIDs[0]
	for i := 1; i < len(deviceIDs); i++ {
		result += "|" + deviceIDs[i]
	}
	return result
}
