// TUI implementation using Bubbletea framework
// Полноценный интерфейс мессенджера в терминале

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/encli-own/encli/pkg/crypto"
)

// Screen определяет текущий экран приложения.
type Screen int

const (
	ScreenChatList Screen = iota
	ScreenChat
	ScreenNewChat
	ScreenSettings
	ScreenAddDevice
	ScreenQRCode
)

// AppModel — главная модель TUI приложения.
type AppModel struct {
	// Криптографическая идентичность
	identity *crypto.Identity

	// Текущий экран
	screen Screen

	// Размеры терминала
	width  int
	height int

	// Компоненты
	chatList list.Model
	viewport viewport.Model
	textarea textarea.Model
	help     help.Model
	keys     keyMap

	// Состояние
	messages      []Message
	conversations []Conversation
	selectedChat  int
	inputValue    string
	statusMessage string
	err           error

	// Network
	network *ClientNetwork

	// Contacts
	contacts *ContactsStore

	// Settings
	serverAddr string
	ephemeral  bool
}

// Message — сообщение в чате.
type Message struct {
	ID        string
	Sender    string
	Content   string
	Timestamp time.Time
	IsOwn     bool
}

// Conversation — диалог.
type Conversation struct {
	ID          string
	Name        string
	LastMessage string
	UnreadCount int
	UpdatedAt   time.Time
	DeviceIDs   []string
}

func (c Conversation) FilterValue() string { return c.Name }

// keyMap — привязки клавиш.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Quit     key.Binding
	Back     key.Binding
	NewChat  key.Binding
	Settings key.Binding
	Send     key.Binding
	Escape   key.Binding
}

var defaultKeyMap = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q/ctrl+c", "quit"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc", "left"),
		key.WithHelp("esc", "back"),
	),
	NewChat: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new chat"),
	),
	Settings: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "settings"),
	),
	Send: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "send"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

// NewAppModel создает новую модель приложения.
func NewAppModel(identity *crypto.Identity) *AppModel {
	// Создаем список чатов
	items := []list.Item{
		Conversation{ID: "welcome", Name: "Welcome", LastMessage: "Welcome to encli!", UnreadCount: 0},
	}

	chatList := list.New(items, conversationDelegate{}, 30, 20)
	chatList.Title = "Conversations"
	chatList.SetShowHelp(false)
	chatList.SetFilteringEnabled(true)
	chatList.Styles.Title = titleStyle

	// Textarea для ввода
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.SetWidth(50)
	ta.SetHeight(3)

	// Viewport для сообщений
	vp := viewport.New(50, 20)

	// Help
	h := help.New()

	contacts := NewContactsStore()
	contacts.Load()

	serverAddr := getSavedServerAddr()

	return &AppModel{
		identity:      identity,
		screen:        ScreenChatList,
		chatList:      chatList,
		textarea:      ta,
		viewport:      vp,
		help:          h,
		keys:          defaultKeyMap,
		conversations: []Conversation{items[0].(Conversation)},
		contacts:      contacts,
		serverAddr:    serverAddr,
		ephemeral:     true,
		network:       NewClientNetwork(),
	}
}

// Init инициализация.
func (m *AppModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.pollMessages(),
		m.tick(),
	)
}

// Update обработка сообщений.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()

	case tea.KeyMsg:
		switch m.screen {
		case ScreenChatList:
			return m.updateChatList(msg)
		case ScreenChat:
			return m.updateChat(msg)
		case ScreenNewChat:
			return m.updateNewChat(msg)
		case ScreenSettings:
			return m.updateSettings(msg)
		}

	case pollMsg:
		cmds = append(cmds, m.pollMessages())

	case tickMsg:
		cmds = append(cmds, m.tick())

	case statusMsg:
		m.statusMessage = string(msg)
		cmds = append(cmds, clearStatusAfter(3*time.Second))

	case clearStatusMsg:
		m.statusMessage = ""
	}

	// Обновляем компоненты
	var cmd tea.Cmd
	m.chatList, cmd = m.chatList.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View отрисовка интерфейса.
func (m *AppModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var content string

	switch m.screen {
	case ScreenChatList:
		content = m.viewChatList()
	case ScreenChat:
		content = m.viewChat()
	case ScreenNewChat:
		content = m.viewNewChat()
	case ScreenSettings:
		content = m.viewSettings()
	default:
		content = m.viewChatList()
	}

	// Статус бар
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar)
}

// --- Chat List Screen ---

func (m *AppModel) updateChatList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Enter):
		if item, ok := m.chatList.SelectedItem().(Conversation); ok {
			m.selectedChat = m.chatList.Index()
			m.screen = ScreenChat
			m.loadMessages(item.ID)
			m.updateSizes()
			m.textarea.Focus()
		}

	case key.Matches(msg, m.keys.NewChat):
		m.screen = ScreenNewChat
		m.textarea.Focus()
		m.textarea.Reset()
		m.textarea.Placeholder = "Enter nickname or device ID..."

	case key.Matches(msg, m.keys.Settings):
		m.screen = ScreenSettings

	default:
		var cmd tea.Cmd
		m.chatList, cmd = m.chatList.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *AppModel) viewChatList() string {
	m.chatList.SetSize(m.width, m.height-2)

	header := titleStyle.Render(" encli ") + subtitleStyle.Render(" — Encrypted CLI Messenger")
	headerBar := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("#1A1A2E")).
		Render(header)

	return lipgloss.JoinVertical(lipgloss.Left, headerBar, m.chatList.View())
}

// --- Chat Screen ---

func (m *AppModel) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.screen = ScreenChatList
		m.textarea.Blur()

	case key.Matches(msg, m.keys.Send):
		m.sendMessage()

	case key.Matches(msg, m.keys.Escape):
		m.screen = ScreenChatList
		m.textarea.Blur()

	default:
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m *AppModel) viewChat() string {
	if m.selectedChat >= len(m.conversations) {
		m.screen = ScreenChatList
		return m.viewChatList()
	}

	conv := m.conversations[m.selectedChat]

	// Header
	header := titleStyle.Render(" ← ") + " " + conv.Name
	headerBar := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("#1A1A2E")).
		Render(header)

	// Messages
	m.viewport.Width = m.width - 4
	m.viewport.Height = m.height - 8

	// Input
	m.textarea.SetWidth(m.width - 4)

	return lipgloss.JoinVertical(lipgloss.Left,
		headerBar,
		m.viewport.View(),
		inputStyle.Render(m.textarea.View()),
	)
}

// --- New Chat Screen ---

func (m *AppModel) updateNewChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Escape):
		m.screen = ScreenChatList
		m.textarea.Blur()
		m.textarea.Reset()

	case key.Matches(msg, m.keys.Enter):
		query := strings.TrimSpace(m.textarea.Value())
		if query == "" {
			return m, nil
		}
		deviceID, err := m.contacts.Resolve(query)
		if err != nil {
			deviceID = query
		}
		if m.serverAddr != "" && len(deviceID) < 32 {
			remoteID := searchDirectory(m.serverAddr, query)
			if remoteID != "" {
				deviceID = remoteID
			}
		}
		m.contacts.Add(query, deviceID)
		conv := Conversation{
			ID:   deviceID,
			Name: query,
		}
		m.conversations = append(m.conversations, conv)
		m.chatList.InsertItem(len(m.conversations)-1, conv)
		m.screen = ScreenChatList
		m.textarea.Blur()
		m.textarea.Reset()

	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) viewNewChat() string {
	header := titleStyle.Render(" New Chat ") + " " + subtitleStyle.Render("Enter nickname or device ID")

	content := lipgloss.NewStyle().
		Width(m.width - 4).
		Height(m.height - 10).
		Render("\n  Enter nickname or device ID:")

	results := ""
	query := strings.TrimSpace(m.textarea.Value())
	if query != "" {
		contacts := m.contacts.Search(query)
		if len(contacts) > 0 {
			var lines []string
			for _, c := range contacts {
				shortID := c.DeviceID
				if len(shortID) > 16 {
					shortID = shortID[:16]
				}
				lines = append(lines, fmt.Sprintf("  %s (%s)", c.Nickname, shortID))
			}
			results = "\n" + strings.Join(lines, "\n")
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		results,
		m.textarea.View(),
	)
}

// --- Settings Screen ---

func (m *AppModel) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Escape):
		m.screen = ScreenChatList
	}
	return m, nil
}

func (m *AppModel) viewSettings() string {
	header := titleStyle.Render(" Settings ")

	info := fmt.Sprintf(`
  Device ID:    %s
  Fingerprint:  %s
  Server:       %s
  Ephemeral:    %v
  
  Press ESC to go back
`,
		func() string {
			s := m.identity.DeviceID
			if len(s) > 16 {
				return s[:16] + "..."
			}
			return s
		}(),
		m.identity.Fingerprint,
		m.serverAddr,
		m.ephemeral,
	)

	content := lipgloss.NewStyle().
		Width(m.width - 4).
		Height(m.height - 6).
		Render(info)

	return lipgloss.JoinVertical(lipgloss.Left, header, content)
}

// --- Helpers ---

func (m *AppModel) updateSizes() {
	switch m.screen {
	case ScreenChat:
		m.viewport.Width = m.width - 4
		m.viewport.Height = m.height - 8
		m.textarea.SetWidth(m.width - 4)
	case ScreenChatList:
		m.chatList.SetSize(m.width, m.height-2)
	}
}

func (m *AppModel) sendMessage() {
	content := strings.TrimSpace(m.textarea.Value())
	if content == "" {
		return
	}

	msg := Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Sender:    "me",
		Content:   content,
		Timestamp: time.Now(),
		IsOwn:     true,
	}

	m.messages = append(m.messages, msg)
	m.textarea.SetValue("")
	m.textarea.Focus()

	// Обновляем viewport
	m.refreshViewport()

	// Отправляем по сети (async)
	go m.network.SendMessage(msg)
}

func (m *AppModel) loadMessages(convID string) {
	// Загружаем из локального хранилища
	m.messages = []Message{
		{
			ID:        "welcome",
			Sender:    "encli",
			Content:   "Welcome to encli! Your encrypted messenger.",
			Timestamp: time.Now(),
			IsOwn:     false,
		},
	}
	m.refreshViewport()
}

func (m *AppModel) refreshViewport() {
	var b strings.Builder
	for _, msg := range m.messages {
		style := otherMessageStyle
		if msg.IsOwn {
			style = ownMessageStyle
		}

		ts := timestampStyle.Render(msg.Timestamp.Format("15:04"))
		sender := senderStyle.Render(msg.Sender)

		line := fmt.Sprintf("%s %s\n%s\n\n", ts, sender, style.Render(msg.Content))
		b.WriteString(line)
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func (m *AppModel) renderStatusBar() string {
	if m.statusMessage != "" {
		return statusBarStyle.Render(" " + m.statusMessage + " ")
	}

	shortcutHelp := m.help.ShortHelpView([]key.Binding{
		m.keys.Quit,
		m.keys.NewChat,
		m.keys.Settings,
		m.keys.Back,
	})

	return statusBarStyle.Render(" " + shortcutHelp + " ")
}

// --- Messages ---

type pollMsg struct{}
type tickMsg struct{}
type statusMsg string
type clearStatusMsg struct{}

func (m *AppModel) pollMessages() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		// Poll server for new messages
		if m.network != nil {
			m.network.PullMessages()
		}
		return pollMsg{}
	})
}

func (m *AppModel) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// --- List Delegate ---

type conversationDelegate struct{}

func (d conversationDelegate) Height() int                             { return 3 }
func (d conversationDelegate) Spacing() int                            { return 1 }
func (d conversationDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d conversationDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	conv, ok := item.(Conversation)
	if !ok {
		return
	}

	name := conv.Name
	lastMsg := conv.LastMessage
	if len(lastMsg) > 40 {
		lastMsg = lastMsg[:37] + "..."
	}

	style := lipgloss.NewStyle().Padding(0, 1)
	if index == m.Index() {
		style = selectedItemStyle.Padding(0, 1)
	}

	unread := ""
	if conv.UnreadCount > 0 {
		unread = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#EF4444")).
			Render(fmt.Sprintf(" [%d]", conv.UnreadCount))
	}

	fmt.Fprintf(w, "%s\n%s\n",
		style.Render(name+unread),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render(lastMsg),
	)
}
