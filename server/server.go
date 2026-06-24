package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/encli-own/encli/pkg/crypto"
	"golang.org/x/crypto/acme/autocert"
)

// Server — blind relay node.
type Server struct {
	config *Config

	// HTTP server
	httpServer *http.Server
	// gRPC server
	grpcListener net.Listener

	// Хранилище
	storage *MemoryStorage

	// Менеджер сессий
	sessionManager *crypto.SessionManager

	// Challenges (для auth)
	challenges   map[string]*pendingChallenge
	challengesMu sync.Mutex

	// Публичные ключи устройств: deviceID -> pubkey
	deviceKeys map[string][]byte
	keysMu     sync.RWMutex

	// Rate limiter
	rateLimiter *RateLimiter

	// API ключ для /stats
	apiKey string

	// Stop channel
	stopCh chan struct{}
}

// NewServer создает новый сервер.
func NewServer(config *Config) (*Server, error) {
	storage := NewMemoryStorage(
		config.Server.MaxMailboxSize,
		config.Server.MessageTTL,
		config.Server.MaxAccounts,
	)

	sessionManager, err := crypto.NewSessionManager()
	if err != nil {
		return nil, fmt.Errorf("session manager: %w", err)
	}

	s := &Server{
		config:         config,
		storage:        storage,
		sessionManager: sessionManager,
		challenges:     make(map[string]*pendingChallenge),
		deviceKeys:     make(map[string][]byte),
		rateLimiter:    NewRateLimiter(config.Server.RateLimit.RequestsPerSecond, config.Server.RateLimit.Burst),
		apiKey:         generateAPIKey(),
		stopCh:         make(chan struct{}),
	}

	// Настраиваем HTTP маршруты
	mux := http.NewServeMux()

	// Публичные endpoints (не требуют auth)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/manifest", s.handleManifest)
	mux.HandleFunc("/v1/auth/challenge", s.handleAuthChallenge)
	mux.HandleFunc("/v1/auth/verify", s.handleAuthVerify)

	// Push/Pull endpoints (mailbox-based, не требуют session token)
	mux.HandleFunc("/v1/push", s.handlePush)
	mux.HandleFunc("/v1/pull", s.handlePull)
	mux.HandleFunc("/v1/peek", s.handlePeek)

	// Noise endpoint
	mux.HandleFunc("/v1/noise", s.handleNoise)

	// Admin endpoints
	mux.HandleFunc("/v1/stats", s.handleStats)

	// WebSocket
	mux.HandleFunc("/v1/ws", s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s, nil
}

// Start запускает HTTP сервер.
func (s *Server) Start() error {
	log.Printf("Starting HTTP server on %s", s.httpServer.Addr)

	if s.config.Server.TLS.Enabled {
		if s.config.Server.TLS.AutoCert {
			// Let's Encrypt
			m := &autocert.Manager{
				Cache:      autocert.DirCache("certs"),
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist( /* добавить домены */ ),
			}
			s.httpServer.TLSConfig = m.TLSConfig()
			return s.httpServer.ListenAndServeTLS("", "")
		}
		return s.httpServer.ListenAndServeTLS(
			s.config.Server.TLS.CertPath,
			s.config.Server.TLS.KeyPath,
		)
	}

	return s.httpServer.ListenAndServe()
}

// StartGRPC запускает gRPC сервер (для федерации).
func (s *Server) StartGRPC() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPCPort)
	log.Printf("Starting gRPC server on %s", addr)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("gRPC listener: %w", err)
	}
	s.grpcListener = listener

	// В реальной реализации: создать gRPC сервер и зарегистрировать сервисы
	// grpcServer := grpc.NewServer()
	// protocol.RegisterFederationServer(grpcServer, s)
	// return grpcServer.Serve(listener)

	// Заглушка — просто слушаем
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return nil
			default:
				return err
			}
		}
		go s.handleGRPCConnection(conn)
	}
}

// handleGRPCConnection обрабатывает gRPC соединение (заглушка).
func (s *Server) handleGRPCConnection(conn net.Conn) {
	defer conn.Close()
	// В реальной реализации: gRPC обработка
	buf := make([]byte, 1024)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

// Shutdown gracefully останавливает сервер.
func (s *Server) Shutdown(ctx context.Context) error {
	close(s.stopCh)

	// Shutdown HTTP
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	// Shutdown gRPC
	if s.grpcListener != nil {
		s.grpcListener.Close()
	}

	return nil
}

// noiseTrafficLoop генерирует шумовой трафик.
func (s *Server) noiseTrafficLoop(ctx context.Context) {
	if !s.config.Server.NoiseTraffic.Enabled {
		return
	}

	ticker := time.NewTimer(noisePacketInterval(
		s.config.Server.NoiseTraffic.MinInterval,
		s.config.Server.NoiseTraffic.MaxInterval,
	))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			// Генерируем шумовой пакет и отправляем на случайный mailbox
			// В реальности: отправляем broadcast на все активные соединения
			s.generateNoiseTraffic()

			// Сбрасываем таймер с новым случайным интервалом
			ticker.Reset(noisePacketInterval(
				s.config.Server.NoiseTraffic.MinInterval,
				s.config.Server.NoiseTraffic.MaxInterval,
			))
		}
	}
}

// generateNoiseTraffic отправляет шумовые пакеты клиентам.
func (s *Server) generateNoiseTraffic() {
	// В реальной реализации: отправка на активные WebSocket соединения
	// Здесь: просто логируем для демонстрации
	// log.Printf("Generated noise traffic packet")
}

// cleanupLoop периодически очищает истекшие сообщения и сессии.
func (s *Server) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(s.config.Server.MailboxCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			deletedMessages, deletedMailboxes := s.storage.CleanupExpired()
			expiredSessions := s.sessionManager.CleanupExpired()
			deletedChallenges := s.cleanupExpiredChallenges()

			if deletedMessages > 0 || deletedMailboxes > 0 {
				log.Printf("Cleanup: %d messages, %d mailboxes, %d sessions, %d challenges removed",
					deletedMessages, deletedMailboxes, expiredSessions, deletedChallenges)
			}
		}
	}
}

// cleanupExpiredChallenges удаляет истекшие challenges.
func (s *Server) cleanupExpiredChallenges() int {
	s.challengesMu.Lock()
	defer s.challengesMu.Unlock()

	now := time.Now().UTC()
	count := 0
	for id, ch := range s.challenges {
		if now.Sub(ch.CreatedAt) > crypto.DefaultChallengeTTL {
			delete(s.challenges, id)
			count++
		}
	}
	return count
}
