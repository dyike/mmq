package storage

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite database connection
type DB struct {
	*sql.DB
}

// New creates a new database connection
func New(dbPath string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	wrapper := &DB{db}

	// Run migrations
	if err := wrapper.migrate(); err != nil {
		return nil, err
	}

	return wrapper, nil
}

// migrate runs database migrations
func (db *DB) migrate() error {
	migrations := []string{
		// Sessions table
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT 'New Session',
			agent_name TEXT NOT NULL,
			agent_session_id TEXT,
			working_directory TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			is_archived INTEGER NOT NULL DEFAULT 0
		)`,

		// Index for listing sessions by date
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at DESC)`,

		// Index for filtering by agent
		`CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_name)`,

		// Messages table
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_use_id TEXT,
			parent_tool_use_id TEXT,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,

		// Index for loading messages by session
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at)`,

		// Tool invocations table
		`CREATE TABLE IF NOT EXISTS tool_invocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			tool_name TEXT NOT NULL,
			tool_input TEXT NOT NULL,
			tool_output TEXT,
			status TEXT NOT NULL,
			approved_at INTEGER,
			completed_at INTEGER,
			error_message TEXT,
			FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
		)`,

		// Index for tool lookup
		`CREATE INDEX IF NOT EXISTS idx_tool_invocations_message ON tool_invocations(message_id)`,

		// Settings table
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return err
		}
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
