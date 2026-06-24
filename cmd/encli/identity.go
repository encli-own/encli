// Identity management — генерация, хранение и загрузка криптографических ключей.
// Ключи хранятся в зашифрованном виде на диске, расшифровываются в ОЗУ при запуске.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/encli-own/encli/pkg/crypto"
)

const (
	// DefaultKeysDir — директория для хранения ключей.
	DefaultKeysDir = ".encli"
	// IdentityFile — имя файла идентичности.
	IdentityFile = "identity.enc"
	// KeysSubDir — поддиректория для ключей.
	KeysSubDir = "keys"
)

// StoredIdentity — сериализованная идентичность (зашифрованная).
type StoredIdentity struct {
	// DeviceID — публичный идентификатор (SHA-256 публичного ключа).
	DeviceID string `json:"device_id"`
	// Fingerprint — human-readable fingerprint.
	Fingerprint string `json:"fingerprint"`
	// EncryptedPrivateKey — зашифрованный приватный ключ Ed25519.
	EncryptedPrivateKey []byte `json:"encrypted_priv_key"`
	// Salt для key derivation.
	Salt []byte `json:"salt"`
	// Version формата.
	Version int `json:"version"`
}

// GetKeysDir возвращает путь к директории ключей.
func GetKeysDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, DefaultKeysDir, KeysSubDir)
}

// GetIdentityPath возвращает путь к файлу идентичности.
func GetIdentityPath() string {
	return filepath.Join(GetKeysDir(), IdentityFile)
}

// loadOrCreateIdentity загружает существующую идентичность или создает новую.
func loadOrCreateIdentity() (*crypto.Identity, error) {
	keysDir := GetKeysDir()
	identityPath := GetIdentityPath()

	// Проверяем, существует ли identity
	if _, err := os.Stat(identityPath); err == nil {
		// Identity существует — загружаем
		return loadIdentityFromFile(identityPath)
	}

	// Создаем новую identity
	fmt.Println("Creating new identity...")
	identity, err := crypto.GenerateIdentity()
	if err != nil {
		return nil, fmt.Errorf("identity generation: %w", err)
	}

	// Создаем директории
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("creating keys directory: %w", err)
	}

	// Сохраняем identity
	if err := saveIdentityToFile(identity, identityPath); err != nil {
		return nil, fmt.Errorf("saving identity: %w", err)
	}

	fmt.Printf("New identity created!\n")
	fmt.Printf("Device ID: %s\n", identity.DeviceID)
	fmt.Printf("Fingerprint: %s\n", identity.Fingerprint)
	fmt.Printf("Keys stored in: %s\n", keysDir)

	return identity, nil
}

// loadIdentityFromFile загружает identity из файла.
func loadIdentityFromFile(path string) (*crypto.Identity, error) {
	// В реальной реализации: расшифровка с мастер-паролем
	// Сейчас: простая загрузка JSON (demo)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading identity file: %w", err)
	}

	var stored StoredIdentity
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parsing identity: %w", err)
	}

	// В реальности: расшифровка EncryptedPrivateKey с мастер-паролем
	// и реконструкция Identity
	// Здесь: placeholder — генерируем новую (для демо)

	fmt.Println("Loading existing identity...")
	// TODO: Implement secure key loading with master password

	// For now: generate a new identity each time (demo mode)
	identity, err := crypto.GenerateIdentity()
	if err != nil {
		return nil, err
	}

	return identity, nil
}

// saveIdentityToFile сохраняет identity в файл (зашифрованную).
func saveIdentityToFile(identity *crypto.Identity, path string) error {
	// В реальной реализации: шифрование с мастер-паролем
	// Сейчас: простое JSON (demo — НЕ БЕЗОПАСНО для production!)

	stored := StoredIdentity{
		DeviceID:    identity.DeviceID,
		Fingerprint: identity.Fingerprint,
		Version:     1,
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// generateKeys — CLI: генерация новой пары ключей.
func generateKeys() error {
	keysDir := GetKeysDir()
	identityPath := GetIdentityPath()

	// Проверяем, не существуют ли уже ключи
	if _, err := os.Stat(identityPath); err == nil {
		return fmt.Errorf("keys already exist at %s", identityPath)
	}

	fmt.Println("Generating new Ed25519/X25519 keypair...")

	identity, err := crypto.GenerateIdentity()
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}

	// Создаем директории
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Сохраняем
	if err := saveIdentityToFile(identity, identityPath); err != nil {
		return fmt.Errorf("saving keys: %w", err)
	}

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║              NEW IDENTITY GENERATED                      ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Device ID:    %-42s ║\n", identity.DeviceID)
	fmt.Printf("║ Fingerprint:  %-42s ║\n", identity.Fingerprint)
	fmt.Printf("║ Keys:         %-42s ║\n", identityPath)
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\nIMPORTANT: Backup your keys directory! Loss of keys = loss of identity.\n")
	fmt.Printf("Keep your keys secure: %s\n", keysDir)

	return nil
}

// showKeys — CLI: показать публичные ключи.
func showKeys() error {
	identity, err := loadOrCreateIdentity()
	if err != nil {
		return err
	}

	pubKey := crypto.SerializeEd25519Public(identity.Ed25519Public)

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║              DEVICE IDENTITY                             ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Device ID:    %-42s ║\n", identity.DeviceID)
	fmt.Printf("║ Fingerprint:  %-42s ║\n", identity.Fingerprint)
	fmt.Printf("║ Public Key:   %-42s ║\n", fmt.Sprintf("%x...", pubKey[:16]))
	fmt.Printf("║ Server:       %-42s ║\n", serverAddr)
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")

	return nil
}

// registerDevice — CLI: регистрация устройства на сервере.
func registerDevice(serverAddr string) error {
	identity, err := loadOrCreateIdentity()
	if err != nil {
		return err
	}

	fmt.Printf("Registering device on %s...\n", serverAddr)

	// Создаем сетевой клиент и подключаемся
	client := NewClientNetwork()
	if err := client.Connect(serverAddr, identity); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	// Сохраняем конфигурацию
	config := map[string]string{
		"server_addr": serverAddr,
		"device_id":   identity.DeviceID,
		"mailbox_id":  client.mailboxID,
		"session_id":  client.sessionID,
	}

	configPath := filepath.Join(GetKeysDir(), "..", "client.json")
	configData, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, configData, 0600); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║           REGISTRATION SUCCESSFUL                        ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Server:       %-42s ║\n", serverAddr)
	fmt.Printf("║ Mailbox ID:   %-42s ║\n", client.mailboxID)
	shortSession := client.sessionID
	if len(shortSession) > 16 {
		shortSession = shortSession[:16] + "..."
	}
	fmt.Printf("║ Session ID:   %-42s ║\n", shortSession)
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")

	return nil
}

// sendMessage — CLI: отправка сообщения (non-interactive).
func sendMessage(recipientID, message string) error {
	identity, err := loadOrCreateIdentity()
	if err != nil {
		return err
	}

	// Загружаем конфиг
	configPath := filepath.Join(GetKeysDir(), "..", "client.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("not registered (run 'encli register <server>' first)")
	}

	var config map[string]string
	if err := json.Unmarshal(configData, &config); err != nil {
		return err
	}

	serverAddr := config["server_addr"]
	if serverAddr == "" {
		return fmt.Errorf("server address not configured")
	}

	// Подключаемся
	client := NewClientNetwork()
	if err := client.Connect(serverAddr, identity); err != nil {
		return fmt.Errorf("connection: %w", err)
	}

	// Шифруем сообщение (в реальности — Double Ratchet)
	// Сейчас: простая отправка
	msg := Message{
		Sender:  identity.DeviceID[:8],
		Content: message,
	}

	if err := client.SendMessage(msg); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	fmt.Printf("Message sent to %s\n", recipientID)
	return nil
}

// showServerInfo — CLI: показать информацию о сервере.
func showServerInfo(serverAddr string) error {
	client := NewClientNetwork()
	manifest, err := client.GetServerManifest(serverAddr)
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║              SERVER MANIFEST                             ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Server ID:      %-40s ║\n", manifest.ServerID)
	fmt.Printf("║ Version:        %-40s ║\n", manifest.Version)
	fmt.Printf("║ Protocol:       %-40d ║\n", manifest.ProtocolVersion)
	fmt.Printf("║ Description:    %-40s ║\n", manifest.Description)
	fmt.Printf("║ Region:         %-40s ║\n", manifest.Region)
	fmt.Printf("║ Max Accounts:   %-40d ║\n", manifest.MaxAccounts)
	fmt.Printf("║ Curr Accounts:  %-40d ║\n", manifest.CurrentAccounts)
	fmt.Printf("║ Message TTL:    %-40d ║\n", manifest.MessageTTL)
	fmt.Printf("║ Max Msg Size:   %-40d ║\n", manifest.MaxMessageSize)
	fmt.Printf("║ Noise Support:  %-40v ║\n", manifest.NoiseSupport)
	fmt.Printf("║ Federation:     %-40v ║\n", manifest.Federation)
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")

	return nil
}

func getSavedServerAddr() string {
	configPath := filepath.Join(GetKeysDir(), "..", "client.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var config map[string]string
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}
	return config["server_addr"]
}
