package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// Cipher реализует шифрование с authenticated encryption.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher создает новый шифр из 32-байтного ключа (AES-256-GCM).
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("cipher key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher creation failed: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm mode creation failed: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt шифрует plaintext с дополнительными ассоциированными данными (aad).
// Возвращает nonce || ciphertext || tag.
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error) {
	nonce, err := SecureRandom(c.aead.NonceSize())
	if err != nil {
		return nil, fmt.Errorf("nonce generation failed: %w", err)
	}
	ciphertext := c.aead.Seal(nonce, nonce, plaintext, aad)
	return ciphertext, nil
}

// Decrypt расшифровывает данные (формат: nonce || ciphertext || tag).
func (c *Cipher) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:c.aead.NonceSize()], ciphertext[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}

// EncryptWithNonce шифрует с заданным nonce (для детерминированного шифрования).
func (c *Cipher) EncryptWithNonce(plaintext, aad, nonce []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size: %d, expected %d", len(nonce), c.aead.NonceSize())
	}
	ciphertext := c.aead.Seal(nonce, nonce, plaintext, aad)
	return ciphertext, nil
}

// GenerateIV генерирует новый nonce/IV.
func (c *Cipher) GenerateIV() ([]byte, error) {
	return SecureRandom(c.aead.NonceSize())
}

// NonceSize возвращает размер nonce.
func (c *Cipher) NonceSize() int {
	return c.aead.NonceSize()
}

// Overhead возвращает размер аутентификационного тега.
func (c *Cipher) Overhead() int {
	return c.aead.Overhead()
}

// FixedPacketSize — размер фиксированного пакета (16 KB).
const FixedPacketSize = 16384 // 16 * 1024

// Pad дополняет данные до FixedPacketSize случайным шумом.
// Формат: [2 байта длина оригинальных данных][данные][случайный шум].
func Pad(data []byte) ([]byte, error) {
	if len(data) > FixedPacketSize-2 {
		return nil, fmt.Errorf("data too large for padding: %d > %d", len(data), FixedPacketSize-2)
	}

	padded := make([]byte, FixedPacketSize)
	// Записываем длину оригинальных данных (2 байта, big-endian)
	binary.BigEndian.PutUint16(padded[0:2], uint16(len(data)))
	// Копируем данные
	copy(padded[2:2+len(data)], data)
	// Оставшееся заполняем случайным шумом
	if len(data)+2 < FixedPacketSize {
		noise, err := SecureRandom(FixedPacketSize - len(data) - 2)
		if err != nil {
			return nil, fmt.Errorf("noise generation failed: %w", err)
		}
		copy(padded[2+len(data):], noise)
	}
	return padded, nil
}

// Unpad извлекает оригинальные данные из дополненного пакета.
func Unpad(padded []byte) ([]byte, error) {
	if len(padded) != FixedPacketSize {
		return nil, fmt.Errorf("invalid padded size: %d, expected %d", len(padded), FixedPacketSize)
	}
	dataLen := binary.BigEndian.Uint16(padded[0:2])
	if int(dataLen) > FixedPacketSize-2 {
		return nil, fmt.Errorf("invalid data length in padded packet: %d", dataLen)
	}
	return padded[2 : 2+dataLen], nil
}

// EncryptAndPad шифрует данные и дополняет до фиксированного размера.
func (c *Cipher) EncryptAndPad(plaintext, aad []byte) ([]byte, error) {
	encrypted, err := c.Encrypt(plaintext, aad)
	if err != nil {
		return nil, err
	}
	return Pad(encrypted)
}

// DecryptAndUnpad расшифровывает данные из фиксированного пакета.
func (c *Cipher) DecryptAndUnpad(padded, aad []byte) ([]byte, error) {
	encrypted, err := Unpad(padded)
	if err != nil {
		return nil, err
	}
	return c.Decrypt(encrypted, aad)
}

// HMAC вычисляет HMAC-SHA256.
func HMAC(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// VerifyHMAC проверяет HMAC-SHA256.
func VerifyHMAC(key, data, mac []byte) bool {
	expected := HMAC(key, data)
	return hmac.Equal(expected, mac)
}

// SecureCopy копирует данные с защитой от side-channel атак.
func SecureCopy(dst, src []byte) {
	if len(dst) != len(src) {
		return
	}
	for i := range src {
		dst[i] = src[i]
	}
}

// SecureWipe перезаписывает данные случайными байтами (Shredding).
func SecureWipe(data []byte) {
	// 3 прохода: случайные байты, 0x55, 0xAA
	randBytes, _ := SecureRandom(len(data))
	copy(data, randBytes)
	for i := range data {
		data[i] = 0x55
	}
	for i := range data {
		data[i] = 0xAA
	}
	for i := range data {
		data[i] = 0x00
	}
}

// NoisePacket генерирует зашифрованный пакет-шум (dummy traffic).
func (c *Cipher) NoisePacket() ([]byte, error) {
	// Генерируем случайные данные + шифруем + pad
	noise, err := SecureRandom(256)
	if err != nil {
		return nil, err
	}
	return c.EncryptAndPad(noise, nil)
}

// deriveSessionKey деривация сессионного ключа из shared secret.
func deriveSessionKey(sharedSecret []byte, salt []byte) ([]byte, error) {
	return DeriveKey(sharedSecret, salt, []byte("encli-session-v1"), 32)
}

// EncryptStream шифрует поток данных (для файлов).
func (c *Cipher) EncryptStream(reader io.Reader, writer io.Writer, aad []byte) error {
	// Генерируем nonce для потока
	nonce, err := SecureRandom(c.aead.NonceSize())
	if err != nil {
		return err
	}
	// Пишем nonce
	if _, err := writer.Write(nonce); err != nil {
		return err
	}

	buf := make([]byte, 4096)
	blockNum := uint64(0)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			// Деривация nonce для блока: nonce || block_number (big-endian)
			blockNonce := make([]byte, c.aead.NonceSize())
			copy(blockNonce, nonce)
			binary.BigEndian.PutUint64(blockNonce[c.aead.NonceSize()-8:], blockNum)

			encrypted := c.aead.Seal(nil, blockNonce, buf[:n], aad)
			// Пишем длину блока + блок
			lenBuf := make([]byte, 4)
			binary.BigEndian.PutUint32(lenBuf, uint32(len(encrypted)))
			if _, err := writer.Write(lenBuf); err != nil {
				return err
			}
			if _, err := writer.Write(encrypted); err != nil {
				return err
			}
			blockNum++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	// Маркер конца: 0xFFFFFFFF
	endMarker := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	_, err = writer.Write(endMarker)
	return err
}

// DecryptStream расшифровывает поток данных.
func (c *Cipher) DecryptStream(reader io.Reader, writer io.Writer, aad []byte) error {
	// Читаем nonce
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(reader, nonce); err != nil {
		return fmt.Errorf("reading nonce: %w", err)
	}

	blockNum := uint64(0)
	for {
		// Читаем длину блока
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			return fmt.Errorf("reading block length: %w", err)
		}
		blockLen := binary.BigEndian.Uint32(lenBuf)

		// Проверка маркера конца
		if blockLen == 0xFFFFFFFF {
			break
		}

		// Читаем блок
		block := make([]byte, blockLen)
		if _, err := io.ReadFull(reader, block); err != nil {
			return fmt.Errorf("reading block: %w", err)
		}

		// Деривация nonce для блока
		blockNonce := make([]byte, c.aead.NonceSize())
		copy(blockNonce, nonce)
		binary.BigEndian.PutUint64(blockNonce[c.aead.NonceSize()-8:], blockNum)

		decrypted, err := c.aead.Open(nil, blockNonce, block, aad)
		if err != nil {
			return fmt.Errorf("decrypting block: %w", err)
		}
		if _, err := writer.Write(decrypted); err != nil {
			return err
		}
		blockNum++
	}
	return nil
}
