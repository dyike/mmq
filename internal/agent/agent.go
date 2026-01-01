package agent

import (
	"context"
)

// StreamEventType represents the type of stream event
type StreamEventType string

const (
	EventTypeText       StreamEventType = "text"
	EventTypeToolUse    StreamEventType = "tool_use"
	EventTypeToolResult StreamEventType = "tool_result"
	EventTypeError      StreamEventType = "error"
	EventTypeDone       StreamEventType = "done"
)

// StreamEvent represents a streaming event from the agent
type StreamEvent struct {
	Type      StreamEventType
	Delta     string // Text delta for streaming
	ToolUse   *ToolUse
	ToolID    string // Tool use ID for results
	Error     string
	SessionID string
}

// ToolUse represents a tool invocation
type ToolUse struct {
	ID    string
	Name  string
	Input map[string]interface{}
}

// PermissionRequest represents a tool permission request
type PermissionRequest struct {
	ToolName    string
	ToolInput   map[string]interface{}
	Description string
	SessionID   string
}

// Agent defines the interface for AI agent implementations
type Agent interface {
	// Name returns the agent identifier
	Name() string

	// DisplayName returns the human-readable name
	DisplayName() string

	// Start initializes the agent process
	Start(ctx context.Context) error

	// Stop terminates the agent process
	Stop() error

	// IsRunning returns whether the agent is active
	IsRunning() bool

	// Send sends a message and returns a channel for streaming responses
	Send(ctx context.Context, prompt string, sessionID string) (<-chan StreamEvent, error)

	// Resume resumes an existing session with a new prompt
	Resume(ctx context.Context, sessionID string, agentSessionID string, prompt string) (<-chan StreamEvent, error)

	// ApprovePermission approves or denies a permission request
	ApprovePermission(ctx context.Context, approved bool) error
}

// Status represents the current status of an agent
type Status string

const (
	StatusIdle     Status = "idle"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)
