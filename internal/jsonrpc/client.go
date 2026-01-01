package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// PendingRequest represents a pending request waiting for response
type PendingRequest struct {
	Response chan *Response
	Error    chan error
}

// Client represents a JSON-RPC client over subprocess stdio
type Client struct {
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	encoder *json.Encoder
	scanner *bufio.Scanner
	pending map[interface{}]*PendingRequest
	nextID  atomic.Int64
	mu      sync.Mutex
	done    chan struct{}

	// Notification handlers
	notifyHandlers map[string][]func(*Notification)
	notifyMu       sync.RWMutex
}

// NewClient creates a new JSON-RPC client
func NewClient(stdin io.WriteCloser, stdout io.ReadCloser) *Client {
	c := &Client{
		stdin:          stdin,
		stdout:         stdout,
		encoder:        json.NewEncoder(stdin),
		scanner:        bufio.NewScanner(stdout),
		pending:        make(map[interface{}]*PendingRequest),
		done:           make(chan struct{}),
		notifyHandlers: make(map[string][]func(*Notification)),
	}

	// Set a larger buffer for scanning (10MB)
	buf := make([]byte, 10*1024*1024)
	c.scanner.Buffer(buf, len(buf))

	// Start reading responses
	go c.readLoop()

	return c
}

// readLoop reads responses from stdout
func (c *Client) readLoop() {
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.IsResponse() {
			c.handleResponse(&msg)
		} else if msg.IsNotification() {
			c.handleNotification(&Notification{
				JSONRPC: msg.JSONRPC,
				Method:  msg.Method,
				Params:  msg.Params,
			})
		}
	}

	close(c.done)
}

// handleResponse handles a response message
func (c *Client) handleResponse(msg *Message) {
	c.mu.Lock()
	pending, ok := c.pending[msg.ID]
	if ok {
		delete(c.pending, msg.ID)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	resp := &Response{
		JSONRPC: msg.JSONRPC,
		ID:      msg.ID,
		Result:  msg.Result,
		Error:   msg.Error,
	}

	select {
	case pending.Response <- resp:
	default:
	}
}

// handleNotification handles a notification message
func (c *Client) handleNotification(notif *Notification) {
	c.notifyMu.RLock()
	handlers := c.notifyHandlers[notif.Method]
	c.notifyMu.RUnlock()

	for _, handler := range handlers {
		handler(notif)
	}
}

// Call sends a request and waits for a response
func (c *Client) Call(ctx context.Context, method string, params interface{}) (*Response, error) {
	id := c.nextID.Add(1)

	req := NewRequest(id, method, params)

	pending := &PendingRequest{
		Response: make(chan *Response, 1),
		Error:    make(chan error, 1),
	}

	c.mu.Lock()
	c.pending[id] = pending
	err := c.encoder.Encode(req)
	c.mu.Unlock()

	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-pending.Response:
		return resp, nil
	case err := <-pending.Error:
		return nil, err
	case <-c.done:
		return nil, fmt.Errorf("connection closed")
	}
}

// Notify sends a notification (no response expected)
func (c *Client) Notify(method string, params interface{}) error {
	notif := NewNotification(method, params)

	c.mu.Lock()
	err := c.encoder.Encode(notif)
	c.mu.Unlock()

	return err
}

// OnNotification registers a handler for notifications of a specific method
func (c *Client) OnNotification(method string, handler func(*Notification)) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.notifyHandlers[method] = append(c.notifyHandlers[method], handler)
}

// Close closes the client
func (c *Client) Close() error {
	if err := c.stdin.Close(); err != nil {
		return err
	}
	<-c.done
	return nil
}

// Done returns a channel that is closed when the client is done
func (c *Client) Done() <-chan struct{} {
	return c.done
}
