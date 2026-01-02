package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/dyike/g3c/internal/agent"
	"github.com/dyike/g3c/internal/config"
)

// Claude implements the Agent interface for Claude Code CLI
type Claude struct {
	config  config.AgentConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	running bool
	mu      sync.Mutex
}

// New creates a new Claude agent
func New(cfg config.AgentConfig) agent.Agent {
	return &Claude{
		config: cfg,
	}
}

// Name returns the agent identifier
func (c *Claude) Name() string {
	return "claude"
}

// DisplayName returns the human-readable name
func (c *Claude) DisplayName() string {
	if c.config.Name != "" {
		return c.config.Name
	}
	return "Claude Code"
}

// Start initializes the agent process
func (c *Claude) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	// Build command
	args := append([]string{}, c.config.Args...)
	c.cmd = exec.CommandContext(ctx, c.config.Executable, args...)

	// Set working directory
	if c.config.WorkDir != "" {
		c.cmd.Dir = c.config.WorkDir
	} else {
		cwd, _ := os.Getwd()
		c.cmd.Dir = cwd
	}

	// Set environment
	env := os.Environ()
	for k, v := range c.config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	c.cmd.Env = env

	// Get pipes
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Stderr for debugging
	c.cmd.Stderr = os.Stderr

	// Start process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
	}

	c.running = true
	return nil
}

// Stop terminates the agent process
func (c *Claude) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false

	if c.stdin != nil {
		c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}

	return nil
}

// IsRunning returns whether the agent is active
func (c *Claude) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// StreamMessage represents the input message format
type StreamMessage struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
}

// StreamOutput represents Claude's stream-json output
type StreamOutput struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`
	Index   int             `json:"index,omitempty"`
	Delta   *StreamDelta    `json:"delta,omitempty"`
	Error   *StreamError    `json:"error,omitempty"`

	// For content_block_start
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
}

type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type StreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Send sends a message and returns a channel for streaming responses
func (c *Claude) Send(ctx context.Context, prompt string, sessionID string) (<-chan agent.StreamEvent, error) {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		if err := c.Start(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
	}
	c.mu.Unlock()

	events := make(chan agent.StreamEvent, 100)

	go func() {
		defer close(events)

		// Send user message as stream-json input
		msg := StreamMessage{
			Type:    "user",
			Content: prompt,
		}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			events <- agent.StreamEvent{
				Type:  agent.EventTypeError,
				Error: fmt.Sprintf("failed to marshal message: %v", err),
			}
			return
		}

		// Write to stdin with newline
		_, err = fmt.Fprintf(c.stdin, "%s\n", msgBytes)
		if err != nil {
			events <- agent.StreamEvent{
				Type:  agent.EventTypeError,
				Error: fmt.Sprintf("failed to send message: %v", err),
			}
			return
		}

		// Read streaming response
		scanner := bufio.NewScanner(c.stdout)
		buf := make([]byte, 10*1024*1024) // 10MB buffer
		scanner.Buffer(buf, len(buf))

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var output StreamOutput
			if err := json.Unmarshal(line, &output); err != nil {
				continue
			}

			event := c.processOutput(&output, sessionID)
			if event != nil {
				events <- *event
				if event.Type == agent.EventTypeDone || event.Type == agent.EventTypeError {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			events <- agent.StreamEvent{
				Type:  agent.EventTypeError,
				Error: fmt.Sprintf("read error: %v", err),
			}
			return
		}

		// If scanner finished without done event
		events <- agent.StreamEvent{
			Type:      agent.EventTypeDone,
			SessionID: sessionID,
		}
	}()

	return events, nil
}

// processOutput converts Claude output to stream events
func (c *Claude) processOutput(output *StreamOutput, sessionID string) *agent.StreamEvent {
	switch output.Type {
	case "content_block_delta":
		if output.Delta != nil && output.Delta.Type == "text_delta" {
			return &agent.StreamEvent{
				Type:      agent.EventTypeText,
				Delta:     output.Delta.Text,
				SessionID: sessionID,
			}
		}

	case "content_block_start":
		if output.ContentBlock != nil && output.ContentBlock.Type == "tool_use" {
			return &agent.StreamEvent{
				Type:      agent.EventTypeToolUse,
				SessionID: sessionID,
				ToolUse: &agent.ToolUse{
					ID:   output.ContentBlock.ID,
					Name: output.ContentBlock.Name,
				},
			}
		}

	case "message_stop", "result":
		return &agent.StreamEvent{
			Type:      agent.EventTypeDone,
			SessionID: sessionID,
		}

	case "error":
		msg := "unknown error"
		if output.Error != nil {
			msg = output.Error.Message
		}
		return &agent.StreamEvent{
			Type:      agent.EventTypeError,
			Error:     msg,
			SessionID: sessionID,
		}
	}

	return nil
}

// Resume resumes an existing session with a new prompt
func (c *Claude) Resume(ctx context.Context, sessionID string, agentSessionID string, prompt string) (<-chan agent.StreamEvent, error) {
	return c.Send(ctx, prompt, sessionID)
}

// ApprovePermission approves or denies a permission request
func (c *Claude) ApprovePermission(ctx context.Context, approved bool) error {
	return nil
}
