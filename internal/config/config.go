package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AgentConfig holds configuration for an agent
type AgentConfig struct {
	Name           string            `json:"name"`
	Executable     string            `json:"executable"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env,omitempty"`
	WorkDir        string            `json:"work_dir,omitempty"`
	PermissionMode string            `json:"permission_mode"` // "ask", "auto", "deny"
}

// TUIConfig holds TUI-specific settings
type TUIConfig struct {
	SidebarWidth   int    `json:"sidebar_width"`
	MarkdownRender bool   `json:"markdown_render"`
	Theme          string `json:"theme"`
}

// Config represents the application configuration
type Config struct {
	DatabasePath string                 `json:"database_path"`
	DefaultAgent string                 `json:"default_agent"`
	Agents       map[string]AgentConfig `json:"agents"`
	TUI          TUIConfig              `json:"tui"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		DatabasePath: "~/.g3c/g3c.db",
		DefaultAgent: "claude",
		Agents: map[string]AgentConfig{
			"claude": {
				Name:       "Claude Code",
				Executable: "claude",
				Args: []string{
					"--print",
					"--output-format", "stream-json",
					"--input-format", "stream-json",
					"--replay-user-messages",
				},
				PermissionMode: "default",
			},
		},
		TUI: TUIConfig{
			SidebarWidth:   30,
			MarkdownRender: true,
			Theme:          "default",
		},
	}
}

// ConfigDir returns the configuration directory path
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".g3c"), nil
}

// ConfigPath returns the configuration file path
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// ExpandPath expands ~ to home directory
func ExpandPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}

// Load loads configuration from the default config file
func Load() (*Config, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	// If config file doesn't exist, create default
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := Save(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Save saves configuration to the default config file
func Save(cfg *Config) error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// GetDatabasePath returns the expanded database path
func (c *Config) GetDatabasePath() (string, error) {
	return ExpandPath(c.DatabasePath)
}
