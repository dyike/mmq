package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dyike/g3c/internal/agent"
	"github.com/dyike/g3c/internal/storage"
)

// FocusedPane represents which pane has focus
type FocusedPane int

const (
	FocusSidebar FocusedPane = iota
	FocusChat
	FocusInput
)

// Model represents the main TUI application state
type Model struct {
	// Components
	viewport viewport.Model
	textarea textarea.Model

	// State
	width       int
	height      int
	focused     FocusedPane
	ready       bool

	// Data
	sessions       []*storage.Session
	currentSession *storage.Session
	messages       []*storage.Message
	sidebarIndex   int

	// Streaming state
	streaming    bool
	streamBuf    strings.Builder
	streamEvents <-chan agent.StreamEvent

	// Dependencies
	agentMgr *agent.Manager
	db       *storage.DB
	ctx      context.Context

	// Error
	err error
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, agentMgr *agent.Manager, db *storage.DB) Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (Enter to send)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0

	return Model{
		textarea:     ta,
		focused:      FocusInput,
		agentMgr:     agentMgr,
		db:           db,
		ctx:          ctx,
		sidebarIndex: 0,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.loadSessions,
	)
}

// loadSessions loads sessions from database
func (m Model) loadSessions() tea.Msg {
	sessions, err := m.db.ListSessions(false)
	if err != nil {
		return ErrorMsg{Err: err}
	}
	return SessionsLoadedMsg{Sessions: sessions}
}

// loadMessages loads messages for current session
func (m Model) loadMessages() tea.Msg {
	if m.currentSession == nil {
		return nil
	}
	messages, err := m.db.GetMessages(m.currentSession.ID)
	if err != nil {
		return ErrorMsg{Err: err}
	}
	return MessagesLoadedMsg{SessionID: m.currentSession.ID, Messages: messages}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Calculate dimensions
		sidebarWidth := 30
		chatWidth := m.width - sidebarWidth - 4
		chatHeight := m.height - 8 // Leave room for input and status

		m.viewport = viewport.New(chatWidth, chatHeight)
		m.viewport.SetContent(m.renderMessages())

		m.textarea.SetWidth(chatWidth)

	case SessionsLoadedMsg:
		m.sessions = msg.Sessions
		// Auto-select first session or create new one
		if len(m.sessions) > 0 {
			return m, m.selectSession(m.sessions[0])
		}

	case SessionCreatedMsg:
		m.sessions = append([]*storage.Session{msg.Session}, m.sessions...)
		m.currentSession = msg.Session
		m.messages = nil
		m.sidebarIndex = 0
		m.viewport.SetContent(m.renderMessages())

	case SessionSelectedMsg:
		m.currentSession = msg.Session
		return m, m.loadMessages

	case MessagesLoadedMsg:
		if m.currentSession != nil && msg.SessionID == m.currentSession.ID {
			m.messages = msg.Messages
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
		}

	case SendMessageMsg:
		// Create session if needed
		if m.currentSession == nil {
			agentName := m.agentMgr.GetActive()
			if agentName == "" {
				agentName = "claude"
			}
			session, err := m.db.CreateSession(agentName, nil)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.currentSession = session
			m.sessions = append([]*storage.Session{session}, m.sessions...)
			m.sidebarIndex = 0
		}
		// Send message
		return m, m.doSendMessage(msg.Content, m.currentSession)

	case streamCmd:
		m.streaming = true
		m.streamEvents = msg.events
		m.streamBuf.Reset()
		m.err = nil
		// Load messages to show user message, then start streaming
		return m, tea.Batch(
			m.loadMessages,
			waitForEvent(msg.events),
		)

	case StreamEventMsg:
		newModel, cmd := m.handleStreamEvent(msg.Event)
		// Continue waiting for next event if still streaming
		if m.streaming && m.streamEvents != nil {
			return newModel, tea.Batch(cmd, waitForEvent(m.streamEvents))
		}
		return newModel, cmd

	case StreamCompleteMsg:
		m.streaming = false
		m.streamEvents = nil
		// Save assistant message
		if m.streamBuf.Len() > 0 {
			content := m.streamBuf.String()
			m.streamBuf.Reset()
			if m.currentSession != nil {
				_, _ = m.db.CreateTextMessage(m.currentSession.ID, storage.MessageTypeAssistant, content)
			}
			return m, m.loadMessages
		}

	case ErrorMsg:
		m.err = msg.Err
		m.streaming = false
		m.streamEvents = nil

	case TickMsg:
		if m.streaming {
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
			return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return TickMsg{}
			})
		}
	}

	return m, tea.Batch(cmds...)
}

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global shortcuts
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		return m, tea.Quit

	case "tab":
		// Cycle focus: Input -> Sidebar -> Chat -> Input
		switch m.focused {
		case FocusInput:
			m.focused = FocusSidebar
			m.textarea.Blur()
		case FocusSidebar:
			m.focused = FocusChat
		case FocusChat:
			m.focused = FocusInput
			m.textarea.Focus()
		}
		return m, nil

	case "ctrl+n":
		// New session
		return m, m.createSession

	case "ctrl+d":
		// Delete current session
		if m.currentSession != nil && m.focused == FocusSidebar {
			return m, m.deleteSession
		}

	case "ctrl+enter", "ctrl+s":
		// Send message
		if m.focused == FocusInput && !m.streaming {
			content := strings.TrimSpace(m.textarea.Value())
			if content != "" {
				m.textarea.Reset()
				return m, m.sendMessage(content)
			}
		}
		return m, nil
	}

	// Handle focus-specific keys
	switch m.focused {
	case FocusSidebar:
		switch msg.String() {
		case "up", "k":
			if m.sidebarIndex > 0 {
				m.sidebarIndex--
				if m.sidebarIndex < len(m.sessions) {
					return m, m.selectSession(m.sessions[m.sidebarIndex])
				}
			}
		case "down", "j":
			if m.sidebarIndex < len(m.sessions)-1 {
				m.sidebarIndex++
				return m, m.selectSession(m.sessions[m.sidebarIndex])
			}
		case "enter":
			if len(m.sessions) > 0 {
				return m, m.selectSession(m.sessions[m.sidebarIndex])
			}
		}
		return m, nil

	case FocusChat:
		// Let viewport handle scrolling
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case FocusInput:
		// Enter to send message
		if msg.String() == "enter" && !m.streaming {
			content := strings.TrimSpace(m.textarea.Value())
			if content != "" {
				m.textarea.Reset()
				return m, m.sendMessage(content)
			}
			return m, nil
		}
		// Pass all other keys to textarea
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleStreamEvent handles streaming events
func (m Model) handleStreamEvent(event agent.StreamEvent) (tea.Model, tea.Cmd) {
	switch event.Type {
	case agent.EventTypeText:
		m.streamBuf.WriteString(event.Delta)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, nil

	case agent.EventTypeDone:
		m.streaming = false
		m.streamEvents = nil
		return m, func() tea.Msg {
			return StreamCompleteMsg{SessionID: event.SessionID}
		}

	case agent.EventTypeError:
		m.err = fmt.Errorf("agent error: %s", event.Error)
		m.streaming = false
		m.streamEvents = nil
		return m, nil
	}

	return m, nil
}

// createSession creates a new session
func (m Model) createSession() tea.Msg {
	agentName := m.agentMgr.GetActive()
	if agentName == "" {
		agentName = "claude"
	}
	session, err := m.db.CreateSession(agentName, nil)
	if err != nil {
		return ErrorMsg{Err: err}
	}
	return SessionCreatedMsg{Session: session}
}

// selectSession selects a session
func (m Model) selectSession(session *storage.Session) tea.Cmd {
	return func() tea.Msg {
		return SessionSelectedMsg{Session: session}
	}
}

// deleteSession deletes the current session
func (m Model) deleteSession() tea.Msg {
	if m.currentSession == nil {
		return nil
	}
	id := m.currentSession.ID
	if err := m.db.DeleteSession(id); err != nil {
		return ErrorMsg{Err: err}
	}
	m.currentSession = nil
	m.messages = nil
	return m.loadSessions()
}

// sendMessage returns a command to send a message
func (m Model) sendMessage(content string) tea.Cmd {
	return func() tea.Msg {
		return SendMessageMsg{Content: content}
	}
}

// doSendMessage actually sends the message to the agent
func (m Model) doSendMessage(content string, session *storage.Session) tea.Cmd {
	return func() tea.Msg {
		// Save user message
		_, err := m.db.CreateTextMessage(session.ID, storage.MessageTypeUser, content)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		// Update session title if first message
		count, _ := m.db.GetMessageCount(session.ID)
		if count == 1 {
			title := storage.GenerateTitle(content)
			_ = m.db.UpdateSessionTitle(session.ID, title)
		}

		// Get agent and send message
		ag, err := m.agentMgr.GetActiveAgent(m.ctx)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		events, err := ag.Send(m.ctx, content, session.ID)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		// Return command to start streaming
		return streamCmd{events: events, sessionID: session.ID}
	}
}

// streamCmd is a command that starts streaming
type streamCmd struct {
	events    <-chan agent.StreamEvent
	sessionID string
}

// waitForEvent returns a command that waits for the next event
func waitForEvent(events <-chan agent.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return StreamCompleteMsg{}
		}
		return StreamEventMsg{Event: event}
	}
}

// View implements tea.Model
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Sidebar
	sidebar := m.renderSidebar()

	// Chat area
	chat := m.renderChat()

	// Input area
	input := m.renderInput()

	// Status bar
	status := m.renderStatusBar()

	// Layout
	mainArea := lipgloss.JoinVertical(lipgloss.Left,
		chat,
		input,
	)

	content := lipgloss.JoinHorizontal(lipgloss.Top,
		sidebar,
		mainArea,
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		content,
		status,
	)
}

// renderSidebar renders the session sidebar
func (m Model) renderSidebar() string {
	var b strings.Builder

	b.WriteString(SidebarTitleStyle.Render("Sessions"))
	b.WriteString("\n")

	for i, session := range m.sessions {
		title := session.Title
		if len(title) > 25 {
			title = title[:22] + "..."
		}

		style := SessionItemStyle
		if i == m.sidebarIndex && m.focused == FocusSidebar {
			style = SessionItemSelectedStyle
		} else if m.currentSession != nil && session.ID == m.currentSession.ID {
			style = SessionItemActiveStyle
		}

		b.WriteString(style.Render(title))
		b.WriteString("\n")
	}

	if len(m.sessions) == 0 {
		b.WriteString(HelpStyle.Render("No sessions\nCtrl+N to create"))
	}

	return SidebarStyle.Width(30).Height(m.height - 3).Render(b.String())
}

// renderChat renders the chat viewport
func (m Model) renderChat() string {
	chatWidth := m.width - 34
	chatHeight := m.height - 8

	style := ChatViewStyle
	if m.focused == FocusChat {
		style = style.BorderForeground(AccentColor)
	}

	return style.Width(chatWidth).Height(chatHeight).Render(m.viewport.View())
}

// renderMessages renders chat messages
func (m Model) renderMessages() string {
	var b strings.Builder

	for _, msg := range m.messages {
		content := msg.GetTextContent()

		switch msg.Type {
		case storage.MessageTypeUser:
			b.WriteString(UserMessageStyle.Render("You: "))
			b.WriteString(content)
		case storage.MessageTypeAssistant:
			b.WriteString(AssistantMessageStyle.Render("Assistant: "))
			b.WriteString(content)
		case storage.MessageTypeSystem:
			b.WriteString(SystemMessageStyle.Render("System: "))
			b.WriteString(content)
		case storage.MessageTypeTool:
			b.WriteString(ToolMessageStyle.Render("Tool: "))
			b.WriteString(content)
		}

		b.WriteString("\n\n")
	}

	// Add streaming content
	if m.streaming && m.streamBuf.Len() > 0 {
		b.WriteString(AssistantMessageStyle.Render("Assistant: "))
		b.WriteString(m.streamBuf.String())
		b.WriteString("▊") // Cursor
	}

	if b.Len() == 0 {
		return HelpStyle.Render("No messages yet.\nType a message and press Ctrl+Enter to send.")
	}

	return b.String()
}

// renderInput renders the input textarea
func (m Model) renderInput() string {
	chatWidth := m.width - 34

	style := InputStyle
	if m.focused == FocusInput {
		style = InputFocusedStyle
	}

	return style.Width(chatWidth).Render(m.textarea.View())
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	agentName := m.agentMgr.GetActive()
	if agentName == "" {
		agentName = "none"
	}

	agentStatus := StatusAgentStyle.Render(agentName)

	var status string
	if m.streaming {
		status = StatusRunningStyle.Render("Streaming...")
	} else if m.err != nil {
		status = StatusErrorStyle.Render(m.err.Error())
	}

	help := HelpStyle.Render("Tab: switch focus | Ctrl+N: new | Ctrl+Enter: send | Ctrl+Q: quit")

	left := lipgloss.JoinHorizontal(lipgloss.Left, agentStatus, " ", status)
	gap := strings.Repeat(" ", max(0, m.width-lipgloss.Width(left)-lipgloss.Width(help)-2))

	return StatusBarStyle.Width(m.width).Render(left + gap + help)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
