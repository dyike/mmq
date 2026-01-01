package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dyike/g3c/internal/agent"
	"github.com/dyike/g3c/internal/agent/claude"
	"github.com/dyike/g3c/internal/config"
	"github.com/dyike/g3c/internal/storage"
	"github.com/dyike/g3c/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize database
	dbPath, err := cfg.GetDatabasePath()
	if err != nil {
		return fmt.Errorf("failed to get database path: %w", err)
	}

	db, err := storage.New(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer db.Close()

	// Initialize agent manager
	agentMgr := agent.NewManager(cfg.Agents)

	// Register agent factories
	agentMgr.RegisterFactory("claude", func(cfg config.AgentConfig) agent.Agent {
		return claude.New(cfg)
	})
	agentMgr.RegisterFactory("default", func(cfg config.AgentConfig) agent.Agent {
		return claude.New(cfg)
	})

	// Set default agent
	if cfg.DefaultAgent != "" {
		if err := agentMgr.SetActive(cfg.DefaultAgent); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set default agent: %v\n", err)
		}
	}

	// Create TUI model
	model := tui.NewModel(ctx, agentMgr, db)

	// Create and run program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Run the TUI
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	// Cleanup
	agentMgr.StopAll()

	return nil
}
