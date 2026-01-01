package storage

import (
	"encoding/json"
	"time"
)

// MessageType represents the type of message
type MessageType string

const (
	MessageTypeUser      MessageType = "user"
	MessageTypeAssistant MessageType = "assistant"
	MessageTypeSystem    MessageType = "system"
	MessageTypeTool      MessageType = "tool"
	MessageTypeResult    MessageType = "result"
)

// ContentBlock represents a block of content
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// For tool use
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
	// For tool result
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
}

// Message represents a stored message
type Message struct {
	ID              int64
	SessionID       string
	Type            MessageType
	Content         []ContentBlock
	ToolUseID       *string
	ParentToolUseID *string
	CreatedAt       time.Time
}

// CreateMessage creates a new message
func (db *DB) CreateMessage(sessionID string, msgType MessageType, content []ContentBlock, toolUseID, parentToolUseID *string) (*Message, error) {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()

	result, err := db.Exec(`
		INSERT INTO messages (session_id, type, content, tool_use_id, parent_tool_use_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sessionID, string(msgType), string(contentJSON), toolUseID, parentToolUseID, now)

	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Touch session to update timestamp
	_ = db.TouchSession(sessionID)

	return &Message{
		ID:              id,
		SessionID:       sessionID,
		Type:            msgType,
		Content:         content,
		ToolUseID:       toolUseID,
		ParentToolUseID: parentToolUseID,
		CreatedAt:       time.Unix(now, 0),
	}, nil
}

// CreateTextMessage is a helper to create a simple text message
func (db *DB) CreateTextMessage(sessionID string, msgType MessageType, text string) (*Message, error) {
	content := []ContentBlock{{Type: "text", Text: text}}
	return db.CreateMessage(sessionID, msgType, content, nil, nil)
}

// GetMessages retrieves all messages for a session
func (db *DB) GetMessages(sessionID string) ([]*Message, error) {
	rows, err := db.Query(`
		SELECT id, session_id, type, content, tool_use_id, parent_tool_use_id, created_at
		FROM messages
		WHERE session_id = ?
		ORDER BY created_at ASC
	`, sessionID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var m Message
		var contentJSON string
		var createdAt int64

		err := rows.Scan(
			&m.ID, &m.SessionID, &m.Type, &contentJSON,
			&m.ToolUseID, &m.ParentToolUseID, &createdAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(contentJSON), &m.Content); err != nil {
			return nil, err
		}

		m.CreatedAt = time.Unix(createdAt, 0)
		messages = append(messages, &m)
	}

	return messages, rows.Err()
}

// GetMessageCount returns the number of messages in a session
func (db *DB) GetMessageCount(sessionID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&count)
	return count, err
}

// DeleteMessage deletes a message by ID
func (db *DB) DeleteMessage(id int64) error {
	_, err := db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return err
}

// GetLastUserMessage retrieves the last user message in a session
func (db *DB) GetLastUserMessage(sessionID string) (*Message, error) {
	var m Message
	var contentJSON string
	var createdAt int64

	err := db.QueryRow(`
		SELECT id, session_id, type, content, tool_use_id, parent_tool_use_id, created_at
		FROM messages
		WHERE session_id = ? AND type = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID, string(MessageTypeUser)).Scan(
		&m.ID, &m.SessionID, &m.Type, &contentJSON,
		&m.ToolUseID, &m.ParentToolUseID, &createdAt,
	)

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(contentJSON), &m.Content); err != nil {
		return nil, err
	}

	m.CreatedAt = time.Unix(createdAt, 0)
	return &m, nil
}

// GetTextContent extracts text content from a message
func (m *Message) GetTextContent() string {
	var text string
	for _, block := range m.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text
}
