package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dyike/mmq/pkg/mmq"
	"github.com/spf13/cobra"
)

// memory 父命令
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage LLM memories",
	Long:  "Store, recall, and manage memories for LLM context (conversations, facts, preferences)",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// --- memory list ---

var memoryListType string

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored memories",
	RunE:  runMemoryList,
}

func runMemoryList(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	if memoryListType != "" {
		// 按类型列出
		memories, err := m.ListMemoriesByType(mmq.MemoryType(memoryListType))
		if err != nil {
			return fmt.Errorf("failed to list memories: %w", err)
		}

		if len(memories) == 0 {
			fmt.Printf("No memories of type '%s' found\n", memoryListType)
			return nil
		}

		outputMemoryList(memories)
		return nil
	}

	// 列出所有类型
	types := []string{"conversation", "fact", "preference", "episodic"}
	totalCount := 0

	for _, t := range types {
		memories, err := m.ListMemoriesByType(mmq.MemoryType(t))
		if err != nil {
			continue
		}
		if len(memories) > 0 {
			fmt.Printf("── %s (%d) ──\n", t, len(memories))
			outputMemoryList(memories)
			fmt.Println()
			totalCount += len(memories)
		}
	}

	if totalCount == 0 {
		fmt.Println("No memories stored yet. Use 'mmq memory add' to create one.")
	} else {
		fmt.Printf("Total: %d memories\n", totalCount)
	}

	return nil
}

// --- memory recall ---

var memoryRecallLimit int

var memoryRecallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Semantic search memories",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMemoryRecall,
}

func runMemoryRecall(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	query := strings.Join(args, " ")
	memories, err := m.RecallMemories(query, mmq.RecallOptions{
		Limit:              memoryRecallLimit,
		ApplyDecay:         true,
		DecayHalflife:      30 * 24 * time.Hour,
		WeightByImportance: true,
	})
	if err != nil {
		return fmt.Errorf("recall failed: %w", err)
	}

	if len(memories) == 0 {
		fmt.Println("No relevant memories found")
		return nil
	}

	fmt.Printf("Found %d relevant memories for \"%s\":\n\n", len(memories), query)
	for i, mem := range memories {
		fmt.Printf("[%d] Type: %s | Importance: %.1f\n", i+1, mem.Type, mem.Importance)
		fmt.Printf("    %s\n", truncate(mem.Content, 200))
		if len(mem.Tags) > 0 {
			fmt.Printf("    Tags: %s\n", strings.Join(mem.Tags, ", "))
		}
		fmt.Printf("    Time: %s\n\n", mem.Timestamp.Format("2006-01-02 15:04"))
	}

	return nil
}

// --- memory add ---

var (
	memoryAddType       string
	memoryAddImportance float64
	memoryAddTags       string
)

var memoryAddCmd = &cobra.Command{
	Use:   "add [content]",
	Short: "Add a memory manually",
	Long:  "Add a memory. Types: conversation, fact, preference, episodic",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMemoryAdd,
}

func runMemoryAdd(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	content := strings.Join(args, " ")

	var tags []string
	if memoryAddTags != "" {
		tags = strings.Split(memoryAddTags, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	mem := mmq.Memory{
		Type:       mmq.MemoryType(memoryAddType),
		Content:    content,
		Tags:       tags,
		Timestamp:  time.Now(),
		Importance: memoryAddImportance,
	}

	if err := m.StoreMemory(mem); err != nil {
		return fmt.Errorf("failed to store memory: %w", err)
	}

	fmt.Printf("✓ Memory stored (type=%s, importance=%.1f)\n", memoryAddType, memoryAddImportance)
	return nil
}

// --- memory delete ---

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a memory by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runMemoryDelete,
}

func runMemoryDelete(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.DeleteMemory(args[0]); err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}

	fmt.Printf("✓ Memory %s deleted\n", args[0])
	return nil
}

// --- memory stats ---

var memoryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show memory statistics",
	RunE:  runMemoryStats,
}

func runMemoryStats(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	total, err := m.CountMemories()
	if err != nil {
		return fmt.Errorf("failed to count memories: %w", err)
	}

	fmt.Printf("📊 Memory Statistics\n\n")
	fmt.Printf("  Total: %d\n", total)

	types := []string{"conversation", "fact", "preference", "episodic"}
	for _, t := range types {
		count, err := m.CountMemoriesByType(mmq.MemoryType(t))
		if err != nil {
			continue
		}
		if count > 0 {
			fmt.Printf("  %-15s %d\n", t+":", count)
		}
	}

	return nil
}

// --- memory cleanup ---

var memoryCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove expired memories",
	RunE:  runMemoryCleanup,
}

func runMemoryCleanup(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	count, err := m.CleanupExpiredMemories()
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	if count == 0 {
		fmt.Println("No expired memories to clean up")
	} else {
		fmt.Printf("✓ Cleaned up %d expired memories\n", count)
	}

	return nil
}

// --- memory get ---

var memoryGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get memory details by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runMemoryGet,
}

func runMemoryGet(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	mem, err := m.GetMemoryByID(args[0])
	if err != nil {
		return fmt.Errorf("memory not found: %w", err)
	}

	if outputFormat == "json" {
		data, _ := json.MarshalIndent(mem, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("ID:         %s\n", mem.ID)
	fmt.Printf("Type:       %s\n", mem.Type)
	fmt.Printf("Content:    %s\n", mem.Content)
	fmt.Printf("Importance: %.2f\n", mem.Importance)
	fmt.Printf("Timestamp:  %s\n", mem.Timestamp.Format("2006-01-02 15:04:05"))
	if len(mem.Tags) > 0 {
		fmt.Printf("Tags:       %s\n", strings.Join(mem.Tags, ", "))
	}
	if mem.ExpiresAt != nil {
		fmt.Printf("Expires:    %s\n", mem.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if len(mem.Metadata) > 0 {
		metaJSON, _ := json.MarshalIndent(mem.Metadata, "            ", "  ")
		fmt.Printf("Metadata:   %s\n", string(metaJSON))
	}

	return nil
}

// --- init ---

func init() {
	// memory list
	memoryListCmd.Flags().StringVar(&memoryListType, "type", "", "Filter by type (conversation|fact|preference|episodic)")
	memoryCmd.AddCommand(memoryListCmd)

	// memory recall
	memoryRecallCmd.Flags().IntVar(&memoryRecallLimit, "limit", 10, "Max results")
	memoryCmd.AddCommand(memoryRecallCmd)

	// memory add
	memoryAddCmd.Flags().StringVar(&memoryAddType, "type", "fact", "Memory type (conversation|fact|preference|episodic)")
	memoryAddCmd.Flags().Float64Var(&memoryAddImportance, "importance", 0.5, "Importance weight 0.0-1.0")
	memoryAddCmd.Flags().StringVar(&memoryAddTags, "tags", "", "Comma-separated tags")
	memoryCmd.AddCommand(memoryAddCmd)

	// memory delete
	memoryCmd.AddCommand(memoryDeleteCmd)

	// memory get
	memoryCmd.AddCommand(memoryGetCmd)

	// memory stats
	memoryCmd.AddCommand(memoryStatsCmd)

	// memory cleanup
	memoryCmd.AddCommand(memoryCleanupCmd)
}

// --- helpers ---

func memoryTypeFromString(s string) mmq.MemoryType {
	switch s {
	case "conversation":
		return mmq.MemoryTypeConversation
	case "fact":
		return mmq.MemoryTypeFact
	case "preference":
		return mmq.MemoryTypePreference
	case "episodic":
		return mmq.MemoryTypeEpisodic
	default:
		return mmq.MemoryType(s)
	}
}

func outputMemoryList(memories []mmq.Memory) {
	for _, mem := range memories {
		snippet := truncate(mem.Content, 80)
		age := time.Since(mem.Timestamp)
		ageStr := formatAge(age)

		fmt.Printf("  [%s] %.1f★ %s (%s)\n", mem.ID[:8], mem.Importance, snippet, ageStr)
	}
}

func truncate(s string, maxLen int) string {
	// 按 rune 截断
	runes := []rune(s)
	if len(runes) <= maxLen {
		return strings.ReplaceAll(s, "\n", " ")
	}
	return strings.ReplaceAll(string(runes[:maxLen]), "\n", " ") + "..."
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
