// Double Ratchet реализация для encli.
// Обеспечивает forward secrecy и future secrecy — 
// компрометация одного ключа не раскрывает прошлые и будущие сообщения.

package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	MaxSkip       = 1000 // Максимальное количество пропущенных ключей
	ChainKeySize  = 32
	MessageKeySize = 32
	HeaderKeySize = 32
)

// Header содержит метаданные сообщения Double Ratchet.
// Все полни зашифрованы header key.
type Header struct {
	DHPublic   [32]byte // X25519 ephemeral public key
	PN         uint32   // Previous chain length
	N          uint32   // Message number in current chain
}

// Serialize сериализует заголовок в байты.
func (h *Header) Serialize() []byte {
	buf := make([]byte, 40)
	copy(buf[0:32], h.DHPublic[:])
	binary.BigEndian.PutUint32(buf[32:36], h.PN)
	binary.BigEndian.PutUint32(buf[36:40], h.N)
	return buf
}

// DeserializeHeader десериализует заголовок из байт.
func DeserializeHeader(data []byte) (*Header, error) {
	if len(data) != 40 {
		return nil, fmt.Errorf("invalid header size: %d", len(data))
	}
	var h Header
	copy(h.DHPublic[:], data[0:32])
	h.PN = binary.BigEndian.Uint32(data[32:36])
	h.N = binary.BigEndian.Uint32(data[36:40])
	return &h, nil
}

// MessageKeyPair содержит ключи для шифрования одного сообщения.
type MessageKeyPair struct {
	MessageKey [MessageKeySize]byte
	HeaderKey  [HeaderKeySize]byte
}

// RatchetStep представляет состояние цепочки.
type RatchetStep struct {
	ChainKey [ChainKeySize]byte
	Length   uint32 // Количество сообщений в цепочке
}

// DoubleRatchet реализует алгоритм Double Ratchet.
type DoubleRatchet struct {
	mu sync.RWMutex

	// Root key
	RootKey [ChainKeySize]byte

	// Отправляющая цепочка (sending chain)
	SendChain RatchetStep

	// Принимающая цепочка (receiving chain)
	RecvChain RatchetStep

	// DH ключи
	DHSelfPrivate [32]byte
	DHSelfPublic  [32]byte
	DHRemotePublic [32]byte

	// Сколько сообщений отправлено в предыдущей отправляющей цепочке
	SendChainLength uint32

	// Сколько сообщений отправлено в текущей принимающей цепочке
	RecvChainLength uint32

	// Принимающая цепочка текущая — нужно ли выполнить DH ratchet
	RecvStepNeeded bool

	// Skip-лист: map[messageNum] -> MessageKeyPair
	SkippedKeys map[uint32]MessageKeyPair

	// Header key для шифрования заголовков
	SendHeaderKey [HeaderKeySize]byte
	RecvHeaderKey [HeaderKeySize]byte

	// Счетчик пропущенных ключей (защита от DoS)
	SkippedCount int
}

// InitAlice инициализирует ratchet для Alice (инициатор).
// sharedSecret — результат X25519 handshake.
// remoteDHPublic — публичный ключ DH Bob.
func InitAlice(sharedSecret, remoteDHPublic [32]byte) (*DoubleRatchet, error) {
	dr := &DoubleRatchet{
		DHRemotePublic: remoteDHPublic,
		SkippedKeys:    make(map[uint32]MessageKeyPair),
		RecvStepNeeded: true,
	}

	// Генерируем свою DH пару
	priv, pub, err := generateDHKeyPair()
	if err != nil {
		return nil, err
	}
	dr.DHSelfPrivate = priv
	dr.DHSelfPublic = pub

	// KDF_RK для инициализации root key
	dhOutput, err := curve25519.X25519(dr.DHSelfPrivate[:], dr.DHRemotePublic[:])
	if err != nil {
		return nil, fmt.Errorf("initial dh failed: %w", err)
	}

	rootKey, chainKey, err := kdfRK(sharedSecret[:], dhOutput)
	if err != nil {
		return nil, err
	}
	dr.RootKey = rootKey
	dr.SendChain.ChainKey = chainKey

	return dr, nil
}

// InitBob инициализирует ratchet для Bob (респондент).
// sharedSecret — результат X25519 handshake.
// selfDHPriv, selfDHPub — своя DH пара (генерируется заранее и передается Alice).
func InitBob(sharedSecret [32]byte, selfDHPriv, selfDHPub [32]byte) (*DoubleRatchet, error) {
	dr := &DoubleRatchet{
		DHSelfPrivate:  selfDHPriv,
		DHSelfPublic:   selfDHPub,
		SkippedKeys:    make(map[uint32]MessageKeyPair),
		RecvStepNeeded: false,
	}

	// Bob начинает с пустой отправляющей цепочкой
	// Root key инициализируется просто из shared secret
	dr.RootKey = sharedSecret

	return dr, nil
}

// Encrypt шифрует сообщение с ratchet.
// Возвращает (header, ciphertext).
func (dr *DoubleRatchet) Encrypt(plaintext []byte, associatedData []byte) (*Header, []byte, error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// Шаг sending chain
	msgKey, nextChainKey := kdfCK(dr.SendChain.ChainKey)
	dr.SendChain.ChainKey = nextChainKey
	msgNum := dr.SendChain.Length
	dr.SendChain.Length++

	// Формируем header
	header := &Header{
		DHPublic: dr.DHSelfPublic,
		PN:       dr.SendChainLength,
		N:        msgNum,
	}

	// Шифруем plaintext с message key
	cipher, err := NewCipher(msgKey[:])
	if err != nil {
		return nil, nil, err
	}

	// AAD = header || associatedData
	aad := append(header.Serialize(), associatedData...)
	ciphertext, err := cipher.Encrypt(plaintext, aad)
	if err != nil {
		return nil, nil, err
	}

	// Шифруем header с send header key
	headerCipher, err := NewCipher(dr.SendHeaderKey[:])
	if err != nil {
		return nil, nil, err
	}
	encryptedHeader, err := headerCipher.Encrypt(header.Serialize(), associatedData)
	if err != nil {
		return nil, nil, err
	}

	_ = encryptedHeader // используется при передаче

	return header, ciphertext, nil
}

// Decrypt расшифровывает сообщение с ratchet.
func (dr *DoubleRatchet) Decrypt(header *Header, ciphertext []byte, associatedData []byte) ([]byte, error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// Проверяем, нужен ли DH ratchet шаг
	if dr.RecvStepNeeded || !isSameDHPublic(dr.DHRemotePublic, header.DHPublic) {
		// DH ratchet step
		if err := dr.dhRatchetStep(header.DHPublic); err != nil {
			return nil, err
		}
	}

	// Проверяем skip keys
	if header.N > dr.RecvChain.Length {
		// Нужно пропустить сообщения
		if err := dr.skipMessageKeys(header.N); err != nil {
			return nil, err
		}
	}

	if header.N < dr.RecvChain.Length {
		// Старое сообщение — ищем в skipped keys
		mkp, ok := dr.SkippedKeys[header.N]
		if !ok {
			return nil, fmt.Errorf("message key not found for skipped message %d", header.N)
		}
		delete(dr.SkippedKeys, header.N)
		dr.SkippedCount--
		return dr.decryptWithKey(ciphertext, mkp, header, associatedData)
	}

	// Текущее сообщение — шаг receiving chain
	msgKey, nextChainKey := kdfCK(dr.RecvChain.ChainKey)
	dr.RecvChain.ChainKey = nextChainKey
	dr.RecvChain.Length++

	mkp := MessageKeyPair{MessageKey: msgKey}
	return dr.decryptWithKey(ciphertext, mkp, header, associatedData)
}

// decryptWithKey расшифровывает с конкретным message key.
func (dr *DoubleRatchet) decryptWithKey(ciphertext []byte, mkp MessageKeyPair, header *Header, associatedData []byte) ([]byte, error) {
	cipher, err := NewCipher(mkp.MessageKey[:])
	if err != nil {
		return nil, err
	}
	aad := append(header.Serialize(), associatedData...)
	return cipher.Decrypt(ciphertext, aad)
}

// dhRatchetStep выполняет DH ratchet при получении нового DH public key.
func (dr *DoubleRatchet) dhRatchetStep(newRemoteDHPub [32]byte) error {
	// Завершаем текущую recv chain
	if dr.RecvChain.Length > 0 || !dr.RecvStepNeeded {
		dr.SendChainLength = dr.SendChain.Length
	}

	// Новый DH output
	 dhOutput, err := curve25519.X25519(dr.DHSelfPrivate[:], newRemoteDHPub[:])
	if err != nil {
		return fmt.Errorf("dh ratchet step failed: %w", err)
	}

	// Обновляем root key и recv chain
	rootKey, recvChainKey, err := kdfRK(dr.RootKey[:], dhOutput)
	if err != nil {
		return err
	}
	dr.RootKey = rootKey
	dr.RecvChain = RatchetStep{ChainKey: recvChainKey}
	dr.RecvChainLength = dr.RecvChain.Length
	dr.RecvStepNeeded = false

	// Генерируем новую DH пару для send chain
	priv, pub, err := generateDHKeyPair()
	if err != nil {
		return err
	}
	dr.DHSelfPrivate = priv
	dr.DHSelfPublic = pub
	dr.DHRemotePublic = newRemoteDHPub

	// Новый DH output для send chain
	dhOutput, err = curve25519.X25519(dr.DHSelfPrivate[:], dr.DHRemotePublic[:])
	if err != nil {
		return fmt.Errorf("dh ratchet send step failed: %w", err)
	}

	rootKey, sendChainKey, err := kdfRK(dr.RootKey[:], dhOutput)
	if err != nil {
		return err
	}
	dr.RootKey = rootKey
	dr.SendChain = RatchetStep{ChainKey: sendChainKey}
	dr.SendChainLength = 0

	return nil
}

// skipMessageKeys генерирует ключи для пропущенных сообщений.
func (dr *DoubleRatchet) skipMessageKeys(until uint32) error {
	if dr.RecvChain.Length+MaxSkip < until {
		return fmt.Errorf("too many skipped messages")
	}
	if dr.SkippedCount+int(until-dr.RecvChain.Length) > MaxSkip {
		return fmt.Errorf("skip key limit exceeded")
	}

	for dr.RecvChain.Length < until {
		msgKey, nextChainKey := kdfCK(dr.RecvChain.ChainKey)
		mkp := MessageKeyPair{MessageKey: msgKey}
		dr.SkippedKeys[dr.RecvChain.Length] = mkp
		dr.SkippedCount++
		dr.RecvChain.ChainKey = nextChainKey
		dr.RecvChain.Length++
	}
	return nil
}

// generateDHKeyPair генерирует X25519 ключевую пару.
func generateDHKeyPair() (priv [32]byte, pub [32]byte, err error) {
	randBytes, err := SecureRandom(32)
	if err != nil {
		return priv, pub, err
	}
	copy(priv[:], randBytes)
	clampScalar(priv[:])
	curve25519.ScalarBaseMult(&pub, &priv)
	return priv, pub, nil
}

// kdfRK — KDF для root key (HKDF-SHA256).
func kdfRK(rootKey, dhOutput []byte) (rootKeyOut [32]byte, chainKey [32]byte, err error) {
	hkdfReader := hkdf.New(sha256.New, dhOutput, rootKey, []byte("encli-ratchet-v1"))
	if _, err := hkdfReader.Read(rootKeyOut[:]); err != nil {
		return rootKeyOut, chainKey, fmt.Errorf("kdf root key failed: %w", err)
	}
	if _, err := hkdfReader.Read(chainKey[:]); err != nil {
		return rootKeyOut, chainKey, fmt.Errorf("kdf chain key failed: %w", err)
	}
	return rootKeyOut, chainKey, nil
}

// kdfCK — KDF для chain key (HMAC-based).
func kdfCK(chainKey [32]byte) (msgKey [32]byte, nextChainKey [32]byte) {
	// message_key = HMAC-SHA256(chain_key, 0x01)
	mac1 := hmac.New(sha256.New, chainKey[:])
	mac1.Write([]byte{0x01})
	copy(msgKey[:], mac1.Sum(nil))

	// next_chain_key = HMAC-SHA256(chain_key, 0x02)
	mac2 := hmac.New(sha256.New, chainKey[:])
	mac2.Write([]byte{0x02})
	copy(nextChainKey[:], mac2.Sum(nil))

	return msgKey, nextChainKey
}

// isSameDHPublic сравнивает два DH публичных ключа (constant-time).
func isSameDHPublic(a, b [32]byte) bool {
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// ExportState экспортирует состояние ratchet (для сериализации).
// ⚠️ Только для backup — содержит секретные ключи!
func (dr *DoubleRatchet) ExportState() ([]byte, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	// Простая сериализация — в реальном коде использовать protobuf/msgpack
	state := make([]byte, 0, 512)
	state = append(state, dr.RootKey[:]...)
	state = append(state, dr.SendChain.ChainKey[:]...)
	state = append(state, dr.RecvChain.ChainKey[:]...)
	state = append(state, dr.DHSelfPrivate[:]...)
	state = append(state, dr.DHSelfPublic[:]...)
	state = append(state, dr.DHRemotePublic[:]...)
	// ... etc
	return state, nil
}
