// encli — TUI клиент для encli messenger
// Консольный мессенджер с E2EE и zero-knowledge архитектурой.

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "encli",
	Short: "Encrypted CLI Messenger",
	Long: `encli — федеративный децентрализованный консольный мессенджер 
с архитектурой нулевого разглашения (Zero-Knowledge) и E2EE.

Авторизация по криптографическим ключам. Без паролей. Без телефонов.
Без сбора метаданных.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Запуск TUI
		runTUI()
	},
}

var (
	serverAddr string
	configPath string
	debugMode  bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&serverAddr, "server", "s", "", "Server address (host:port)")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Config file path")
	rootCmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug mode")

	// Subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(registerCmd)
	rootCmd.AddCommand(sendCmd)
	rootCmd.AddCommand(keysCmd)
	rootCmd.AddCommand(serverInfoCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("encli %s (built %s)\n", Version, BuildTime)
	},
}

var registerCmd = &cobra.Command{
	Use:   "register [server-address]",
	Short: "Register device on server",
	Long:  `Register this device on the specified server. Generates keys if needed.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := registerDevice(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Registration failed: %v\n", err)
			os.Exit(1)
		}
	},
}

var sendCmd = &cobra.Command{
	Use:   "send [device-id] [message]",
	Short: "Send a message (non-interactive)",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		if err := sendMessage(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Send failed: %v\n", err)
			os.Exit(1)
		}
	},
}

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Manage cryptographic keys",
}

var keysGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate new device keypair",
	Run: func(cmd *cobra.Command, args []string) {
		if err := generateKeys(); err != nil {
			fmt.Fprintf(os.Stderr, "Key generation failed: %v\n", err)
			os.Exit(1)
		}
	},
}

var keysShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show device public key and fingerprint",
	Run: func(cmd *cobra.Command, args []string) {
		if err := showKeys(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
			os.Exit(1)
		}
	},
}

var serverInfoCmd = &cobra.Command{
	Use:   "info [server-address]",
	Short: "Get server manifest info",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := showServerInfo(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	keysCmd.AddCommand(keysGenerateCmd)
	keysCmd.AddCommand(keysShowCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runTUI запускает полноценный TUI интерфейс.
func runTUI() {
	// Проверяем, существуют ли ключи
	identity, err := loadOrCreateIdentity()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load identity: %v\n", err)
		fmt.Println("Run: encli keys generate")
		os.Exit(1)
	}

	// Создаем TUI model
	model := NewAppModel(identity)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// Styles для TUI.
var (
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		Background(lipgloss.Color("#1A1A2E")).
		Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A0A0B0"))

	senderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#6FCF97"))

	ownMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F2F2F2")).
		Background(lipgloss.Color("#2D2D44")).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4"))

	otherMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F2F2F2")).
		Background(lipgloss.Color("#1E1E30")).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4A5568"))

	timestampStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Italic(true)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)

	listStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#4A5568")).
		Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		Background(lipgloss.Color("#2D2D44"))

	inputStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#7D56F4"))

	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Bold(true)

	successStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981"))
)
