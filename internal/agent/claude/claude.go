package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/dyike/g3c/internal/agent"
	"github.com/dyike/g3c/internal/config"
	"github.com/dyike/g3c/internal/jsonrpc"
)

// Claude implements the Agent interface for Claude Code CLI via ACP
type Claude struct {
	config    config.AgentConfig
	cmd       *exec.Cmd
	client    *jsonrpc.Client
	running   bool
	mu        sync.Mutex
	sessionID string

	// Permission handling
	permissionCh chan bool
}

// New creates a new Claude agent
func New(cfg config.AgentConfig) agent.Agent {
	return &Claude{
		config:       cfg,
		permissionCh: make(chan bool, 1),
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
		// Use current working directory
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
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Stderr for debugging
	c.cmd.Stderr = os.Stderr

	// Start process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
	}

	// Create JSON-RPC client
	c.client = jsonrpc.NewClient(stdin, stdout)
	c.running = true

	// Initialize ACP
	if err := c.initialize(ctx); err != nil {
		c.Stop()
		return fmt.Errorf("failed to initialize ACP: %w", err)
	}

	return nil
}

// initialize performs ACP handshake
func (c *Claude) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2025-01-01",
		"capabilities": map[string]interface{}{
			"prompts":   map[string]interface{}{},
			"resources": map[string]interface{}{},
			"tools":     map[string]interface{}{},
		},
		"clientInfo": map[string]interface{}{
			"name":    "g3c",
			"version": "0.1.0",
		},
	}

	resp, err := c.client.Call(ctx, "initialize", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return resp.Error
	}

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

	if c.client != nil {
		c.client.Close()
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

		// Create new session if needed
		if c.sessionID == "" {
			if err := c.createSession(ctx); err != nil {
				events <- agent.StreamEvent{
					Type:  agent.EventTypeError,
					Error: fmt.Sprintf("failed to create session: %v", err),
				}
				return
			}
		}

		// Register notification handlers
		textCh := make(chan string, 100)
		doneCh := make(chan struct{})
		errCh := make(chan string, 1)

		c.client.OnNotification("notifications/message", func(notif *jsonrpc.Notification) {
			c.handleNotification(notif, textCh, doneCh, errCh)
		})

		// Send prompt
		params := map[string]interface{}{
			"sessionId": c.sessionID,
			"prompt":    prompt,
		}

		// Make call in goroutine since it blocks until complete
		go func() {
			resp, err := c.client.Call(ctx, "session/prompt", params)
			if err != nil {
				errCh <- fmt.Sprintf("prompt error: %v", err)
				return
			}
			if resp.Error != nil {
				errCh <- fmt.Sprintf("prompt error: %s", resp.Error.Message)
				return
			}
			close(doneCh)
		}()

		// Stream events
		for {
			select {
			case text := <-textCh:
				events <- agent.StreamEvent{
					Type:      agent.EventTypeText,
					Delta:     text,
					SessionID: sessionID,
				}
			case errMsg := <-errCh:
				events <- agent.StreamEvent{
					Type:      agent.EventTypeError,
					Error:     errMsg,
					SessionID: sessionID,
				}
				return
			case <-doneCh:
				events <- agent.StreamEvent{
					Type:      agent.EventTypeDone,
					SessionID: sessionID,
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return events, nil
}

// createSession creates a new ACP session
func (c *Claude) createSession(ctx context.Context) error {
	cwd, _ := os.Getwd()
	params := map[string]interface{}{
		"workingDirectory": cwd,
	}

	resp, err := c.client.Call(ctx, "session/new", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return resp.Error
	}

	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return err
	}

	c.sessionID = result.SessionID
	return nil
}

// handleNotification processes ACP notifications
func (c *Claude) handleNotification(notif *jsonrpc.Notification, textCh chan<- string, doneCh chan struct{}, errCh chan<- string) {
	var params struct {
		SessionID string `json:"sessionId"`
		Message   struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		} `json:"message"`
	}

	if err := json.Unmarshal(notif.Params, &params); err != nil {
		return
	}

	// Extract text content
	for _, content := range params.Message.Content {
		if content.Type == "text" && content.Text != "" {
			select {
			case textCh <- content.Text:
			default:
			}
		}
	}
}

// Resume resumes an existing session with a new prompt
func (c *Claude) Resume(ctx context.Context, sessionID string, agentSessionID string, prompt string) (<-chan agent.StreamEvent, error) {
	c.sessionID = agentSessionID
	return c.Send(ctx, prompt, sessionID)
}

// ApprovePermission approves or denies a permission request
func (c *Claude) ApprovePermission(ctx context.Context, approved bool) error {
	select {
	case c.permissionCh <- approved:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
