package tui

import (
	"github.com/dyike/g3c/internal/agent"
	"github.com/dyike/g3c/internal/storage"
)

// SessionsLoadedMsg indicates sessions have been loaded
type SessionsLoadedMsg struct {
	Sessions []*storage.Session
}

// SessionCreatedMsg indicates a new session was created
type SessionCreatedMsg struct {
	Session *storage.Session
}

// SessionSelectedMsg indicates a session was selected
type SessionSelectedMsg struct {
	Session *storage.Session
}

// SessionDeletedMsg indicates a session was deleted
type SessionDeletedMsg struct {
	SessionID string
}

// MessagesLoadedMsg indicates messages have been loaded
type MessagesLoadedMsg struct {
	SessionID string
	Messages  []*storage.Message
}

// StreamEventMsg wraps a streaming event from an agent
type StreamEventMsg struct {
	Event agent.StreamEvent
}

// StreamStartMsg indicates streaming has started
type StreamStartMsg struct {
	SessionID string
}

// StreamCompleteMsg indicates streaming has completed
type StreamCompleteMsg struct {
	SessionID string
}

// PermissionRequestMsg indicates a permission request
type PermissionRequestMsg struct {
	Request agent.PermissionRequest
}

// PermissionResponseMsg indicates a permission response
type PermissionResponseMsg struct {
	Approved bool
}

// AgentStatusMsg indicates agent status change
type AgentStatusMsg struct {
	AgentName string
	Status    agent.Status
}

// ErrorMsg represents an error
type ErrorMsg struct {
	Err error
}

// FocusChangeMsg indicates focus should change
type FocusChangeMsg struct {
	Pane FocusedPane
}

// SendMessageMsg indicates user wants to send a message
type SendMessageMsg struct {
	Content string
}

// TickMsg is sent periodically for animations
type TickMsg struct{}
