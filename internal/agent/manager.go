package agent

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/dyike/g3c/internal/config"
)

// Factory is a function that creates an agent from config
type Factory func(cfg config.AgentConfig) Agent

// Manager manages multiple agent instances
type Manager struct {
	agents    map[string]Agent
	factories map[string]Factory
	configs   map[string]config.AgentConfig
	active    string
	mu        sync.RWMutex
}

// NewManager creates a new agent manager
func NewManager(configs map[string]config.AgentConfig) *Manager {
	return &Manager{
		agents:    make(map[string]Agent),
		factories: make(map[string]Factory),
		configs:   configs,
	}
}

// RegisterFactory registers a factory function for creating agents
func (m *Manager) RegisterFactory(name string, factory Factory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[name] = factory
}

// GetAgent returns an agent by name, creating it if necessary
func (m *Manager) GetAgent(ctx context.Context, name string) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if agent already exists
	if agent, ok := m.agents[name]; ok {
		if !agent.IsRunning() {
			if err := agent.Start(ctx); err != nil {
				return nil, fmt.Errorf("failed to start agent %s: %w", name, err)
			}
		}
		return agent, nil
	}

	// Get config
	cfg, ok := m.configs[name]
	if !ok {
		return nil, fmt.Errorf("agent %s not configured", name)
	}

	// Get factory
	factory, ok := m.factories[name]
	if !ok {
		// Use default factory based on agent type
		factory, ok = m.factories["default"]
		if !ok {
			return nil, fmt.Errorf("no factory registered for agent %s", name)
		}
	}

	// Create agent
	agent := factory(cfg)
	m.agents[name] = agent

	// Start agent
	if err := agent.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start agent %s: %w", name, err)
	}

	return agent, nil
}

// SetActive sets the currently active agent
func (m *Manager) SetActive(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.configs[name]; !ok {
		return fmt.Errorf("agent %s not configured", name)
	}

	m.active = name
	return nil
}

// GetActive returns the currently active agent name
func (m *Manager) GetActive() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// GetActiveAgent returns the currently active agent instance
func (m *Manager) GetActiveAgent(ctx context.Context) (Agent, error) {
	m.mu.RLock()
	active := m.active
	m.mu.RUnlock()

	if active == "" {
		return nil, fmt.Errorf("no active agent set")
	}

	return m.GetAgent(ctx, active)
}

// ListAgents returns a list of configured agent names
func (m *Manager) ListAgents() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name := range m.configs {
		names = append(names, name)
	}
	return names
}

// StopAll stops all running agents
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, agent := range m.agents {
		if agent.IsRunning() {
			_ = agent.Stop()
		}
	}
}

// Route determines which agent should handle a message based on mentions
var mentionRegex = regexp.MustCompile(`@(\w+)`)

// Route selects an agent based on message content
func (m *Manager) Route(message string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check for @mentions
	matches := mentionRegex.FindStringSubmatch(message)
	if len(matches) > 1 {
		mention := matches[1]
		if _, ok := m.configs[mention]; ok {
			return mention
		}
	}

	// Return active agent
	return m.active
}

// GetConfig returns the configuration for an agent
func (m *Manager) GetConfig(name string) (config.AgentConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.configs[name]
	return cfg, ok
}
