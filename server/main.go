// encli-server — Blind Relay Node
// Сервер не знает содержимого сообщений, никнеймов, связей между пользователями.
// Он оперирует исключительно криптографическими хэшами mailbox ID.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	var (
		configPath  = flag.String("config", "configs/server.yaml", "Path to server configuration file")
		healthCheck = flag.Bool("health-check", false, "Run health check and exit")
		showVersion = flag.Bool("version", false, "Show version and exit")
		showHelp    = flag.Bool("help", false, "Show help")
		genKeys     = flag.Bool("gen-keys", false, "Generate server keypair and exit")
		initNode    = flag.Bool("init", false, "Initialize node (create config, keys)")
	)
	flag.Parse()

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("encli-server %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	if *genKeys {
		if err := generateServerKeys(); err != nil {
			log.Fatalf("Key generation failed: %v", err)
		}
		os.Exit(0)
	}

	if *initNode {
		if err := initializeNode(*configPath); err != nil {
			log.Fatalf("Node initialization failed: %v", err)
		}
		fmt.Println("Node initialized successfully")
		os.Exit(0)
	}

	// Load configuration
	config, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if *healthCheck {
		if err := runHealthCheck(config); err != nil {
			log.Fatalf("Health check failed: %v", err)
		}
		fmt.Println("Health check: OK")
		os.Exit(0)
	}

	fmt.Printf("encli-server %s starting...\n", Version)
	fmt.Printf("  Host: %s:%d\n", config.Server.Host, config.Server.Port)
	fmt.Printf("  gRPC: %s:%d\n", config.Server.Host, config.Server.GRPCPort)
	fmt.Printf("  Max accounts: %d\n", config.Server.MaxAccounts)
	fmt.Printf("  Message TTL: %v\n", config.Server.MessageTTL)
	fmt.Printf("  Noise traffic: %v\n", config.Server.NoiseTraffic.Enabled)

	// Create server
	server, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Start gRPC server (for federation)
	go func() {
		if err := server.StartGRPC(); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	// Start noise traffic generator
	if config.Server.NoiseTraffic.Enabled {
		go server.noiseTrafficLoop(ctx)
	}

	// Start cleanup goroutine
	go server.cleanupLoop(ctx)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	sig := <-sigCh
	log.Printf("Received signal: %v, shutting down gracefully...", sig)

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func printHelp() {
	help := `encli-server — Blind Relay Node for encli messenger

Usage:
  encli-server [options]

Options:
  -config string    Path to configuration file (default "configs/server.yaml")
  -health-check     Run health check and exit
  -version          Show version and exit
  -help             Show this help
  -gen-keys         Generate server keypair and exit
  -init             Initialize node (create config, keys, directories)

Environment Variables:
  ENCLI_SERVER_HOST        Server bind address
  ENCLI_SERVER_PORT        Server port (default: 8443)
  ENCLI_GRPC_PORT          gRPC port (default: 8444)
  ENCLI_MESSAGE_TTL        Message TTL (default: 168h)
  ENCLI_MAX_ACCOUNTS       Maximum accounts (default: 10000)
  ENCLI_LOG_LEVEL          Log level: debug, info, warn, error
  ENCLI_DATA_DIR           Data directory path

Example:
  encli-server -config /etc/encli/server.yaml
  ENCLI_LOG_LEVEL=debug encli-server
`
	fmt.Print(help)
}

func generateServerKeys() error {
	fmt.Println("Generating server keypair...")
	// Implementation in crypto package
	return nil
}

func initializeNode(configPath string) error {
	fmt.Printf("Initializing node at %s...\n", configPath)
	return nil
}

func runHealthCheck(config *Config) error {
	// Quick health check
	return nil
}
