package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config — полная конфигурация сервера.
type Config struct {
	Server struct {
		Host              string        `yaml:"host"`
		Port              int           `yaml:"port"`
		GRPCPort          int           `yaml:"grpc_port"`
		TLS               TLSConfig     `yaml:"tls"`
		MaxAccounts       int           `yaml:"max_accounts"`
		MaxDevicesPerAccount int        `yaml:"max_devices_per_account"`
		MaxMessageSize    int           `yaml:"max_message_size"`
		MaxMailboxSize    int           `yaml:"max_mailbox_size"`
		MessageTTL        time.Duration `yaml:"message_ttl"`
		MailboxCleanupInterval time.Duration `yaml:"mailbox_cleanup_interval"`
		NoiseTraffic      NoiseConfig   `yaml:"noise_traffic"`
		RateLimit         RateLimitConfig `yaml:"rate_limit"`
		Federation        FederationConfig `yaml:"federation"`
		Manifest          ManifestConfig `yaml:"manifest"`
		Logging           LoggingConfig `yaml:"logging"`
		Storage           StorageConfig `yaml:"storage"`
	} `yaml:"server"`
}

// TLSConfig — конфигурация TLS.
type TLSConfig struct {
	Enabled   bool   `yaml:"enabled"`
	CertPath  string `yaml:"cert_path"`
	KeyPath   string `yaml:"key_path"`
	AutoCert  bool   `yaml:"auto_cert"`
}

// NoiseConfig — конфигурация шумового трафика.
type NoiseConfig struct {
	Enabled      bool          `yaml:"enabled"`
	MinInterval  time.Duration `yaml:"min_interval"`
	MaxInterval  time.Duration `yaml:"max_interval"`
}

// RateLimitConfig — rate limiting.
type RateLimitConfig struct {
	RequestsPerSecond int `yaml:"requests_per_second"`
	Burst             int `yaml:"burst"`
}

// FederationConfig — федерация.
type FederationConfig struct {
	Enabled          bool          `yaml:"enabled"`
	TrustedNodesFile string        `yaml:"trusted_nodes_file"`
	SyncInterval     time.Duration `yaml:"sync_interval"`
}

// ManifestConfig — публичный манифест сервера.
type ManifestConfig struct {
	ServerID    string `yaml:"server_id"`
	Version     string `yaml:"version"`
	Operator    string `yaml:"operator"`
	Description string `yaml:"description"`
	Region      string `yaml:"region"`
}

// LoggingConfig — логирование.
type LoggingConfig struct {
	Level    string `yaml:"level"`
	Format   string `yaml:"format"`
	Output   string `yaml:"output"`
	FilePath string `yaml:"file_path"`
}

// StorageConfig — хранилище.
type StorageConfig struct {
	Type            string `yaml:"type"`
	PersistencePath string `yaml:"persistence_path"`
	EncryptionKey   string `yaml:"encryption_key"`
}

// LoadConfig загружает конфигурацию из файла и переменных окружения.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Override with environment variables
	applyEnvOverrides(&config)

	return &config, nil
}

// applyEnvOverrides применяет переменные окружения поверх конфига.
func applyEnvOverrides(config *Config) {
	if v := os.Getenv("ENCLI_SERVER_HOST"); v != "" {
		config.Server.Host = v
	}
	if v := os.Getenv("ENCLI_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			config.Server.Port = port
		}
	}
	if v := os.Getenv("ENCLI_GRPC_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			config.Server.GRPCPort = port
		}
	}
	if v := os.Getenv("ENCLI_MESSAGE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Server.MessageTTL = d
		}
	}
	if v := os.Getenv("ENCLI_MAX_ACCOUNTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			config.Server.MaxAccounts = n
		}
	}
	if v := os.Getenv("ENCLI_LOG_LEVEL"); v != "" {
		config.Server.Logging.Level = v
	}
}
