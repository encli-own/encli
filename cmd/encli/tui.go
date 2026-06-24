// TUI implementation using Bubbletea framework
// Полноценный интерфейс мессенджера в терминале

package main

import (
	"encoding/hex"
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

	// Search results (new chat)
	searchResults  []searchResult
	selectedResult int

	// Settings
	serverAddr  string
	ephemeral   bool
	settingsTab int // 0=Identity, 1=Hotkeys, 2=Server
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

// searchResult — результат поиска контакта.
type searchResult struct {
	Nickname string
	DeviceID string
}

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
	Delete   key.Binding
	CtrlD    key.Binding
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
		key.WithKeys("esc"),
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
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete chat"),
	),
	CtrlD: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "delete contact"),
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
		identity:       identity,
		screen:         ScreenChatList,
		chatList:       chatList,
		textarea:       ta,
		viewport:       vp,
		help:           h,
		keys:           defaultKeyMap,
		conversations:  []Conversation{items[0].(Conversation)},
		searchResults:  nil,
		selectedResult: 0,
		contacts:       contacts,
		serverAddr:     serverAddr,
		ephemeral:      true,
		network:        NewClientNetwork(),
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

	case messagesPulledMsg:
		for _, pm := range msg.messages {
			payloadBytes, decErr := hex.DecodeString(pm.Payload)
			if decErr != nil {
				continue
			}
			payload := string(payloadBytes)
			var senderID, msgContent string
			if idx := strings.Index(payload, "\n"); idx > 0 {
				senderID = payload[:idx]
				msgContent = payload[idx+1:]
			} else if idx := strings.Index(payload, ": "); idx > 0 {
				senderID = payload[:idx]
				msgContent = payload[idx+2:]
			} else {
				senderID = "unknown"
				msgContent = payload
			}
			senderName := senderID
			if m.contacts != nil {
				for _, c := range m.contacts.Search(senderID) {
					if c.DeviceID == senderID || (len(senderID) >= 8 && strings.HasPrefix(c.DeviceID, senderID[:8])) {
						senderName = c.Nickname
						break
					}
				}
			}
			if senderName == senderID && len(senderID) > 16 {
				senderName = senderID[:16] + ".."
			}
			m.messages = append(m.messages, Message{
				ID:        pm.MessageID,
				Sender:    senderName,
				Content:   msgContent,
				Timestamp: time.Unix(pm.Timestamp, 0),
				IsOwn:     false,
			})
		}
		m.refreshViewport()
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
		m.searchResults = nil
		m.selectedResult = 0
		m.screen = ScreenNewChat
		m.textarea.Focus()
		m.textarea.Reset()
		m.textarea.Placeholder = "Enter nickname or device ID..."

	case key.Matches(msg, m.keys.Settings):
		m.screen = ScreenSettings

	case key.Matches(msg, m.keys.Delete):
		if item, ok := m.chatList.SelectedItem().(Conversation); ok && item.ID != "welcome" {
			for i, c := range m.conversations {
				if c.ID == item.ID {
					m.conversations = append(m.conversations[:i], m.conversations[i+1:]...)
					break
				}
			}
			for i, c := range m.chatList.Items() {
				if conv, ok := c.(Conversation); ok && conv.ID == item.ID {
					m.chatList.RemoveItem(i)
					break
				}
			}
			return m, m.setStatus("Chat deleted: " + item.Name)
		}

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
		m.searchResults = nil
		m.selectedResult = 0

	case key.Matches(msg, m.keys.CtrlD):
		if len(m.searchResults) > 0 {
			sel := m.searchResults[m.selectedResult]
			m.contacts.Delete(sel.Nickname)
			m.updateSearchResults()
			return m, m.setStatus("Contact deleted: " + sel.Nickname)
		}

	case key.Matches(msg, m.keys.Enter):
		query := strings.TrimSpace(m.textarea.Value())
		if query == "" {
			return m, nil
		}

		var deviceID string
		var nickname string

		if len(m.searchResults) > 0 {
			sel := m.searchResults[m.selectedResult]
			deviceID = sel.DeviceID
			nickname = sel.Nickname
		} else {
			deviceID = query
			nickname = query
			if m.serverAddr != "" && len(deviceID) < 32 {
				remoteID := searchDirectory(m.serverAddr, query)
				if remoteID != "" {
					deviceID = remoteID
				}
			}
		}

		m.contacts.Add(nickname, deviceID)
		conv := Conversation{
			ID:   deviceID,
			Name: nickname,
		}
		m.conversations = append(m.conversations, conv)
		m.chatList.InsertItem(len(m.conversations)-1, conv)
		m.screen = ScreenChatList
		m.textarea.Blur()
		m.textarea.Reset()
		m.searchResults = nil
		m.selectedResult = 0

	default:
		if len(m.searchResults) > 0 && (key.Matches(msg, m.keys.Up) || key.Matches(msg, m.keys.Down)) {
			if key.Matches(msg, m.keys.Up) && m.selectedResult > 0 {
				m.selectedResult--
			}
			if key.Matches(msg, m.keys.Down) && m.selectedResult < len(m.searchResults)-1 {
				m.selectedResult++
			}
			return m, nil
		}
		var cmd tea.Cmd
		oldVal := m.textarea.Value()
		m.textarea, cmd = m.textarea.Update(msg)
		if m.textarea.Value() != oldVal {
			m.updateSearchResults()
		}
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) updateSearchResults() {
	query := strings.TrimSpace(m.textarea.Value())
	m.selectedResult = 0
	if query == "" {
		m.searchResults = nil
		return
	}
	contacts := m.contacts.Search(query)
	m.searchResults = make([]searchResult, len(contacts))
	for i, c := range contacts {
		m.searchResults[i] = searchResult{Nickname: c.Nickname, DeviceID: c.DeviceID}
	}
}

func (m *AppModel) viewNewChat() string {
	header := titleStyle.Render(" New Chat ") + " " + subtitleStyle.Render("Enter nickname or device ID")

	var b strings.Builder
	b.WriteString("\n  Enter nickname or device ID:\n\n")

	if len(m.searchResults) > 0 {
		for i, r := range m.searchResults {
			shortID := r.DeviceID
			if len(shortID) > 16 {
				shortID = shortID[:16]
			}
			line := fmt.Sprintf("  %s (%s)", r.Nickname, shortID)
			if i == m.selectedResult {
				b.WriteString(selectedItemStyle.Render("▸ "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
	}

	content := lipgloss.NewStyle().
		Width(m.width - 4).
		Height(m.height - 10).
		Render(b.String())

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		m.textarea.View(),
	)
}

// --- Settings Screen ---

func (m *AppModel) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Escape):
		m.screen = ScreenChatList
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case msg.Type == tea.KeyTab:
		m.settingsTab = (m.settingsTab + 1) % 3
	case msg.Type == tea.KeyShiftTab:
		m.settingsTab = (m.settingsTab - 1 + 3) % 3
	}
	return m, nil
}

func (m *AppModel) viewSettings() string {
	header := titleStyle.Render(" Settings ")
	tabs := []string{" Identity ", " Hotkeys ", " Server "}
	var tabBar strings.Builder
	for i, t := range tabs {
		if i == m.settingsTab {
			tabBar.WriteString(selectedItemStyle.Render(t))
		} else {
			tabBar.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(t))
		}
		tabBar.WriteString(" ")
	}

	info := ""
	switch m.settingsTab {
	case 0:
		shortID := m.identity.DeviceID
		if len(shortID) > 24 {
			shortID = shortID[:24] + "..."
		}
		info = fmt.Sprintf(`

  Device ID:    %s
  Fingerprint:  %s
  Server:       %s
  Ephemeral:    %v
  Nickname:     %s

  Use "encli profile publish <nickname>" to set your nickname.
`, shortID, m.identity.Fingerprint, m.serverAddr, m.ephemeral, getNickname())

	case 1:
		info = `
  Navigation:
    ↑/k, ↓/j     Navigate lists
    Enter         Select / open chat
    Esc           Go back
    n             New chat
    s             Settings
    d             Delete chat
    q / Ctrl+C    Quit

  Chat:
    Ctrl+S        Send message
    Tab          Switch settings tabs

  Search (new chat):
    ↑/k, ↓/j     Navigate results
    Enter         Select contact
    Ctrl+D        Delete contact from local storage
`
	case 2:
		info = fmt.Sprintf(`
  Server:       %s

  This server is a blind relay node.
  It cannot read your messages or see your contacts.

  Message storage is ephemeral:
  messages are deleted after being pulled.

  End-to-end encryption is not yet implemented.
  Messages are currently sent as plaintext.
`, m.serverAddr)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, tabBar.String(), info)

	content := lipgloss.NewStyle().
		Width(m.width - 4).
		Height(m.height - 6).
		Render(body)

	footer := subtitleStyle.Render(" Tab/Shift+Tab: switch tabs  |  Esc: back ")

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
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

	if m.selectedChat >= len(m.conversations) {
		return
	}
	conv := m.conversations[m.selectedChat]
	recipientID := conv.ID
	if len(conv.DeviceIDs) > 0 {
		recipientID = conv.DeviceIDs[0]
	}

	msg := Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Sender:    m.identity.DeviceID,
		Content:   content,
		Timestamp: time.Now(),
		IsOwn:     true,
	}

	m.messages = append(m.messages, msg)
	m.textarea.SetValue("")
	m.textarea.Focus()

	m.refreshViewport()

	if m.network.mailboxID == "" {
		if err := m.network.Connect(m.serverAddr, m.identity); err != nil {
			return
		}
	}
	if err := m.network.SendMessage(recipientID, msg); err != nil {
		m.statusMessage = "Send failed: " + err.Error()
	}
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

func (m *AppModel) setStatus(msg string) tea.Cmd {
	m.statusMessage = msg
	return clearStatusAfter(3 * time.Second)
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
type messagesPulledMsg struct {
	messages []PulledMessage
}

func (m *AppModel) pollMessages() tea.Cmd {
	return func() tea.Msg {
		if m.network.mailboxID == "" {
			if err := m.network.Connect(m.serverAddr, m.identity); err != nil {
				time.Sleep(10 * time.Second)
				return pollMsg{}
			}
		}
		msgs, err := m.network.PullMessages()
		if err != nil {
			time.Sleep(10 * time.Second)
			return pollMsg{}
		}
		if len(msgs) == 0 {
			time.Sleep(10 * time.Second)
			return pollMsg{}
		}
		return messagesPulledMsg{messages: msgs}
	}
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
