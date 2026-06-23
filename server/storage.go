package main

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Message хранит одно зашифрованное сообщение в памяти сервера.
// Сервер не знает содержимого — только opaque blob.
type Message struct {
	// MailboxID получателя (SHA-256 хеш — сервер не знает владельца)
	MailboxID string
	// ID сообщения
	MessageID string
	// Зашифрованные данные (opaque blob для сервера)
	Payload []byte
	// Зашифрованный заголовок Double Ratchet
	EncryptedHeader []byte
	// Время создания
	CreatedAt time.Time
	// TTL — время жизни
	TTL time.Duration
	// Размер оригинального пакета (для статистики)
	OriginalSize int
}

// Mailbox — изолированный почтовый ящик устройства.
// Каждое устройство имеет свой mailbox — сервер не знает,
// какому аккаунту или пользователю он принадлежит.
type Mailbox struct {
	// ID почтового ящика (SHA-256(DeviceID || ServerSalt))
	ID string
	// Очередь сообщений (FIFO)
	Messages *list.List
	// Максимальное количество сообщений
	MaxSize int
	// mu защищает доступ к сообщениям
	mu sync.RWMutex
	// LastAccess — время последнего доступа (для cleanup)
	LastAccess time.Time
}

// NewMailbox создает новый mailbox.
func NewMailbox(id string, maxSize int) *Mailbox {
	return &Mailbox{
		ID:         id,
		Messages:   list.New(),
		MaxSize:    maxSize,
		LastAccess: time.Now().UTC(),
	}
}

// Push добавляет сообщение в mailbox.
// Если mailbox переполнен — удаляет старейшее сообщение.
func (mb *Mailbox) Push(msg *Message) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Проверяем переполнение
	if mb.Messages.Len() >= mb.MaxSize {
		// Удаляем самое старое сообщение
		if front := mb.Messages.Front(); front != nil {
			mb.Messages.Remove(front)
		}
	}

	mb.Messages.PushBack(msg)
	mb.LastAccess = time.Now().UTC()
	return nil
}

// Pull извлекает все сообщения из mailbox (FIFO).
// После извлечения сообщения удаляются (эфемерное хранение).
func (mb *Mailbox) Pull() []*Message {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	messages := make([]*Message, 0, mb.Messages.Len())
	for e := mb.Messages.Front(); e != nil; {
		msg := e.Value.(*Message)
		messages = append(messages, msg)
		next := e.Next()
		mb.Messages.Remove(e)
		e = next
	}

	mb.LastAccess = time.Now().UTC()
	return messages
}

// Peek просматривает сообщения без удаления.
func (mb *Mailbox) Peek() []*Message {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	messages := make([]*Message, 0, mb.Messages.Len())
	for e := mb.Messages.Front(); e != nil; e = e.Next() {
		messages = append(messages, e.Value.(*Message))
	}
	return messages
}

// Count возвращает количество сообщений.
func (mb *Mailbox) Count() int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.Messages.Len()
}

// MemoryStorage — in-memory хранилище с TTL cleanup.
type MemoryStorage struct {
	mu sync.RWMutex
	// mailboxes: map[mailboxID] -> *Mailbox
	mailboxes map[string]*Mailbox
	// maxMailboxSize — макс. сообщений в ящике
	maxMailboxSize int
	// defaultTTL — время жизни сообщений
	defaultTTL time.Duration
	// accountCount — счетчик зарегистрированных аккаунтов
	accountCount int
	// maxAccounts — максимальное количество аккаунтов
	maxAccounts int
}

// NewMemoryStorage создает новое in-memory хранилище.
func NewMemoryStorage(maxMailboxSize int, defaultTTL time.Duration, maxAccounts int) *MemoryStorage {
	return &MemoryStorage{
		mailboxes:      make(map[string]*Mailbox),
		maxMailboxSize: maxMailboxSize,
		defaultTTL:     defaultTTL,
		maxAccounts:    maxAccounts,
	}
}

// RegisterDevice регистрирует новое устройство (создает mailbox).
func (ms *MemoryStorage) RegisterDevice(deviceID string) (string, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.accountCount >= ms.maxAccounts {
		return "", fmt.Errorf("maximum number of accounts reached: %d", ms.maxAccounts)
	}

	// Генерируем MailboxID
	mailboxID := ms.generateMailboxID(deviceID)

	// Проверяем, не существует ли уже
	if _, exists := ms.mailboxes[mailboxID]; exists {
		return mailboxID, nil // Уже зарегистрирован
	}

	mb := NewMailbox(mailboxID, ms.maxMailboxSize)
	ms.mailboxes[mailboxID] = mb
	ms.accountCount++

	return mailboxID, nil
}

// GetMailbox возвращает mailbox по ID.
func (ms *MemoryStorage) GetMailbox(mailboxID string) (*Mailbox, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	mb, ok := ms.mailboxes[mailboxID]
	return mb, ok
}

// StoreMessage сохраняет сообщение в mailbox.
func (ms *MemoryStorage) StoreMessage(mailboxID string, msg *Message) error {
	mb, ok := ms.GetMailbox(mailboxID)
	if !ok {
		return fmt.Errorf("mailbox not found: %s", mailboxID)
	}

	msg.MailboxID = mailboxID
	if msg.TTL == 0 {
		msg.TTL = ms.defaultTTL
	}
	msg.CreatedAt = time.Now().UTC()

	return mb.Push(msg)
}

// RetrieveMessages извлекает все сообщения из mailbox.
// Сообщения удаляются после извлечения (эфемерное хранение).
func (ms *MemoryStorage) RetrieveMessages(mailboxID string) ([]*Message, error) {
	mb, ok := ms.GetMailbox(mailboxID)
	if !ok {
		return nil, fmt.Errorf("mailbox not found: %s", mailboxID)
	}

	return mb.Pull(), nil
}

// CleanupExpired удаляет сообщения с истекшим TTL и неиспользуемые mailboxes.
func (ms *MemoryStorage) CleanupExpired() (deletedMessages int, deletedMailboxes int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now().UTC()

	for mboxID, mb := range ms.mailboxes {
		mb.mu.Lock()

		// Удаляем истекшие сообщения
		var next *list.Element
		for e := mb.Messages.Front(); e != nil; e = next {
			next = e.Next()
			msg := e.Value.(*Message)
			if now.Sub(msg.CreatedAt) > msg.TTL {
				// Secure wipe payload
				secureWipe(msg.Payload)
				secureWipe(msg.EncryptedHeader)
				mb.Messages.Remove(e)
				deletedMessages++
			}
		}

		mb.mu.Unlock()

		// Удаляем пустые mailboxes, не использовавшиеся долго
		if mb.Messages.Len() == 0 && now.Sub(mb.LastAccess) > ms.defaultTTL*2 {
			delete(ms.mailboxes, mboxID)
			ms.accountCount--
			deletedMailboxes++
		}
	}

	return deletedMessages, deletedMailboxes
}

// Stats возвращает статистику хранилища.
func (ms *MemoryStorage) Stats() StorageStats {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	stats := StorageStats{
		TotalMailboxes: len(ms.mailboxes),
		TotalAccounts:  ms.accountCount,
	}

	for _, mb := range ms.mailboxes {
		count := mb.Count()
		stats.TotalMessages += count
		if count > stats.MaxMailboxMessages {
			stats.MaxMailboxMessages = count
		}
	}

	return stats
}

// StorageStats — статистика хранилища.
type StorageStats struct {
	TotalMailboxes       int
	TotalMessages        int
	TotalAccounts        int
	MaxMailboxMessages   int
}

// generateMailboxID генерирует mailbox ID из deviceID.
func (ms *MemoryStorage) generateMailboxID(deviceID string) string {
	// Добавляем случайную соль для уникальности на разных серверах
	salt := time.Now().UTC().UnixNano()
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d", deviceID, salt)
	return hex.EncodeToString(h.Sum(nil))
}

// secureWipe безопасно очищает память.
func secureWipe(data []byte) {
	if data == nil {
		return
	}
	// Простая реализация — в production использовать более сложный алгоритм
	for i := range data {
		data[i] = 0
	}
	for i := range data {
		data[i] = 0xFF
	}
	for i := range data {
		data[i] = 0x55
	}
	for i := range data {
		data[i] = 0
	}
}
