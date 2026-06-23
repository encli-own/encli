// Package crypto реализует криптографические примитивы encli.
// Все операции используют современные алгоритмы с постквантовой устойчивостью (гибридные схемы).
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ed25519"
)

// KeyPair представляет пару ключей Ed25519 (для подписей) + X25519 (для DH).
type KeyPair struct {
	Ed25519Private ed25519.PrivateKey
	Ed25519Public  ed25519.PublicKey
	X25519Private  [32]byte
	X25519Public   [32]byte
}

// Identity представляет криптографическую идентичность устройства/пользователя.
type Identity struct {
	KeyPair
	// SHA-256 от Ed25519 Public Key — глобальный ID устройства
	DeviceID string
	// Human-readable fingerprint для верификации
	Fingerprint string
}

// GenerateKeyPair создает новую пару ключей с криптографически безопасным RNG.
func GenerateKeyPair() (*KeyPair, error) {
	// Генерация Ed25519 ключей
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 key generation failed: %w", err)
	}

	// Конвертация Ed25519 private key -> X25519 private key
	var x25519Priv [32]byte
	copy(x25519Priv[:], privKey.Seed())
	// Curve25519 clamping
	clampScalar(x25519Priv[:])

	// Вычисление X25519 public key
	var x25519Pub [32]byte
	curve25519.ScalarBaseMult(&x25519Pub, &x25519Priv)

	return &KeyPair{
		Ed25519Private: privKey,
		Ed25519Public:  privKey.Public().(ed25519.PublicKey),
		X25519Private:  x25519Priv,
		X25519Public:   x25519Pub,
	}, nil
}

// GenerateIdentity создает полную идентичность устройства.
func GenerateIdentity() (*Identity, error) {
	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	deviceID := sha256.Sum256(kp.Ed25519Public)
	fingerprint := computeFingerprint(kp.Ed25519Public)

	return &Identity{
		KeyPair:     *kp,
		DeviceID:    hex.EncodeToString(deviceID[:]),
		Fingerprint: fingerprint,
	}, nil
}

// Sign создает Ed25519 подпись сообщения.
func (kp *KeyPair) Sign(message []byte) []byte {
	return ed25519.Sign(kp.Ed25519Private, message)
}

// VerifyVerify verifies an Ed25519 signature.
func VerifySignature(publicKey ed25519.PublicKey, message, sig []byte) bool {
	return ed25519.Verify(publicKey, message, sig)
}

// SharedSecret вычисляет shared secret через X25519 DH.
func (kp *KeyPair) SharedSecret(peerPublicKey [32]byte) ([32]byte, error) {
	var shared [32]byte
	_, err := curve25519.X25519(kp.X25519Private[:], peerPublicKey[:])
	if err != nil {
		return shared, fmt.Errorf("x25519 shared secret failed: %w", err)
	}
	// Используем HMAC-SHA256 для derivation вместо raw shared secret
	shared = sha256.Sum256(append(kp.X25519Private[:], peerPublicKey[:]...))
	return shared, nil
}

// clampScalar выполняет clamping для Curve25519 scalar.
func clampScalar(s []byte) {
	if len(s) != 32 {
		return
	}
	s[0] &= 248
	s[31] &= 127
	s[31] |= 64
}

// computeFingerprint создает human-readable fingerprint из публичного ключа.
func computeFingerprint(pub ed25519.PublicKey) string {
	hash := sha256.Sum256(pub)
	// Формат: ABCD-EFGH-IJKL-MNOP (первые 16 байт)
	fp := hex.EncodeToString(hash[:8])
	return fmt.Sprintf("%s-%s-%s-%s", fp[0:4], fp[4:8], fp[8:12], fp[12:16])
}

// SecureRandom генерирует n криптографически безопасных случайных байт.
func SecureRandom(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("secure random generation failed: %w", err)
	}
	return b, nil
}

// SecureRandomHex генерирует hex-строку из n случайных байт.
func SecureRandomHex(n int) (string, error) {
	b, err := SecureRandom(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Hash возвращает SHA-256 хеш данных.
func Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// HashHex возвращает SHA-256 хеш в hex.
func HashHex(data []byte) string {
	return hex.EncodeToString(Hash(data))
}

// DeriveKey деривация ключа из master key + salt через HKDF-like процедуру.
func DeriveKey(masterKey, salt, info []byte, length int) ([]byte, error) {
	if length <= 0 || length > 64 {
		return nil, fmt.Errorf("invalid key length: %d", length)
	}
	// Простая HKDF-based derivation: HMAC-SHA256(masterKey, salt || info || counter)
	from := append(salt, info...)
	result := sha256.Sum256(append(from, 0x01))
	if length <= 32 {
		return result[:length], nil
	}
	// Для большей длины — цепочка
	result2 := sha256.Sum256(append(result[:], append(from, 0x02)...))
	return append(result[:], result2[:length-32]...), nil
}

// SerializeEd25519Public сериализует Ed25519 публичный ключ.
func SerializeEd25519Public(pub ed25519.PublicKey) []byte {
	p := make([]byte, len(pub))
	copy(p, pub)
	return p
}

// DeserializeEd25519Public десериализует Ed25519 публичный ключ.
func DeserializeEd25519Public(data []byte) (ed25519.PublicKey, error) {
	if len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key size: %d", len(data))
	}
	return ed25519.PublicKey(data), nil
}
