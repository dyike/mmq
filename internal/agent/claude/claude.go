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
	fmt.Fprintf(os.Stderr, "[DEBUG] Claude process started, PID: %d\n", c.cmd.Process.Pid)
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

// StreamOutput represents Claude's stream-json output
type StreamOutput struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"is_error,omitempty"`

	// For assistant message
	Message *AssistantMessage `json:"message,omitempty"`
}

// AssistantMessage represents the assistant's message
type AssistantMessage struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a content block in the message
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Send sends a message and returns a channel for streaming responses
func (c *Claude) Send(ctx context.Context, prompt string, sessionID string) (<-chan agent.StreamEvent, error) {
	events := make(chan agent.StreamEvent, 100)

	go func() {
		defer close(events)

		fmt.Fprintf(os.Stderr, "[DEBUG] Creating new Claude process for prompt: %s\n", prompt)

		// Create a new process for each message (--print mode requires this)
		args := append([]string{}, c.config.Args...)
		cmd := exec.CommandContext(ctx, c.config.Executable, args...)

		// Set working directory
		if c.config.WorkDir != "" {
			cmd.Dir = c.config.WorkDir
		} else {
			cwd, _ := os.Getwd()
			cmd.Dir = cwd
		}

		// Set environment
		env := os.Environ()
		for k, v := range c.config.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env

		// Get pipes
		stdin, err := cmd.StdinPipe()
		if err != nil {
			events <- agent.StreamEvent{Type: agent.EventTypeError, Error: err.Error()}
			return
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			events <- agent.StreamEvent{Type: agent.EventTypeError, Error: err.Error()}
			return
		}

		cmd.Stderr = os.Stderr

		// Start process
		if err := cmd.Start(); err != nil {
			events <- agent.StreamEvent{Type: agent.EventTypeError, Error: err.Error()}
			return
		}
		fmt.Fprintf(os.Stderr, "[DEBUG] Claude process started, PID: %d\n", cmd.Process.Pid)

		// Write prompt and close stdin
		_, err = fmt.Fprintf(stdin, "%s", prompt)
		if err != nil {
			events <- agent.StreamEvent{Type: agent.EventTypeError, Error: err.Error()}
			return
		}
		stdin.Close()
		fmt.Fprintf(os.Stderr, "[DEBUG] Prompt sent and stdin closed\n")

		// Read streaming response
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 10*1024*1024)
		scanner.Buffer(buf, len(buf))

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			fmt.Fprintf(os.Stderr, "[DEBUG] Raw: %s\n", string(line))

			var output StreamOutput
			if err := json.Unmarshal(line, &output); err != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] Parse error: %v\n", err)
				continue
			}

			event := c.processOutput(&output, sessionID)
			if event != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] Event: %+v\n", event)
				events <- *event
				if event.Type == agent.EventTypeDone || event.Type == agent.EventTypeError {
					cmd.Wait()
					return
				}
			}
		}

		cmd.Wait()

		if err := scanner.Err(); err != nil {
			events <- agent.StreamEvent{Type: agent.EventTypeError, Error: err.Error()}
			return
		}

		events <- agent.StreamEvent{Type: agent.EventTypeDone, SessionID: sessionID}
	}()

	return events, nil
}

// processOutput converts Claude output to stream events
func (c *Claude) processOutput(output *StreamOutput, sessionID string) *agent.StreamEvent {
	switch output.Type {
	case "assistant":
		// Extract text from message content
		if output.Message != nil {
			var text string
			for _, block := range output.Message.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
			if text != "" {
				return &agent.StreamEvent{
					Type:      agent.EventTypeText,
					Delta:     text,
					SessionID: sessionID,
				}
			}
		}

	case "result":
		if output.IsError {
			return &agent.StreamEvent{
				Type:      agent.EventTypeError,
				Error:     output.Result,
				SessionID: sessionID,
			}
		}
		return &agent.StreamEvent{
			Type:      agent.EventTypeDone,
			SessionID: sessionID,
		}

	case "system":
		// Ignore system messages (init, etc.)
		return nil
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
