package storage

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Session represents a conversation session
type Session struct {
	ID               string
	Title            string
	AgentName        string
	AgentSessionID   *string
	WorkingDirectory *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	IsArchived       bool
}

// CreateSession creates a new session
func (db *DB) CreateSession(agentName string, workingDir *string) (*Session, error) {
	id := uuid.New().String()
	now := time.Now().Unix()

	_, err := db.Exec(`
		INSERT INTO sessions (id, title, agent_name, working_directory, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, "New Session", agentName, workingDir, now, now)

	if err != nil {
		return nil, err
	}

	return db.GetSession(id)
}

// GetSession retrieves a session by ID
func (db *DB) GetSession(id string) (*Session, error) {
	var s Session
	var createdAt, updatedAt int64
	var isArchived int

	err := db.QueryRow(`
		SELECT id, title, agent_name, agent_session_id, working_directory, created_at, updated_at, is_archived
		FROM sessions WHERE id = ?
	`, id).Scan(
		&s.ID, &s.Title, &s.AgentName, &s.AgentSessionID,
		&s.WorkingDirectory, &createdAt, &updatedAt, &isArchived,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	s.CreatedAt = time.Unix(createdAt, 0)
	s.UpdatedAt = time.Unix(updatedAt, 0)
	s.IsArchived = isArchived == 1

	return &s, nil
}

// ListSessions retrieves all sessions ordered by updated_at
func (db *DB) ListSessions(includeArchived bool) ([]*Session, error) {
	query := `
		SELECT id, title, agent_name, agent_session_id, working_directory, created_at, updated_at, is_archived
		FROM sessions
	`
	if !includeArchived {
		query += " WHERE is_archived = 0"
	}
	query += " ORDER BY updated_at DESC"

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		var createdAt, updatedAt int64
		var isArchived int

		err := rows.Scan(
			&s.ID, &s.Title, &s.AgentName, &s.AgentSessionID,
			&s.WorkingDirectory, &createdAt, &updatedAt, &isArchived,
		)
		if err != nil {
			return nil, err
		}

		s.CreatedAt = time.Unix(createdAt, 0)
		s.UpdatedAt = time.Unix(updatedAt, 0)
		s.IsArchived = isArchived == 1

		sessions = append(sessions, &s)
	}

	return sessions, rows.Err()
}

// UpdateSessionTitle updates the session title
func (db *DB) UpdateSessionTitle(id, title string) error {
	now := time.Now().Unix()
	_, err := db.Exec(`
		UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?
	`, title, now, id)
	return err
}

// UpdateSessionAgentSessionID updates the agent session ID
func (db *DB) UpdateSessionAgentSessionID(id string, agentSessionID string) error {
	now := time.Now().Unix()
	_, err := db.Exec(`
		UPDATE sessions SET agent_session_id = ?, updated_at = ? WHERE id = ?
	`, agentSessionID, now, id)
	return err
}

// TouchSession updates the session's updated_at timestamp
func (db *DB) TouchSession(id string) error {
	now := time.Now().Unix()
	_, err := db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now, id)
	return err
}

// ArchiveSession marks a session as archived
func (db *DB) ArchiveSession(id string) error {
	_, err := db.Exec(`UPDATE sessions SET is_archived = 1 WHERE id = ?`, id)
	return err
}

// DeleteSession deletes a session and its messages
func (db *DB) DeleteSession(id string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// GenerateTitle generates a title from the first message content
func GenerateTitle(content string) string {
	maxLen := 50
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}
