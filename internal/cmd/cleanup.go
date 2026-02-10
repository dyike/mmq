package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// cleanup 命令 - 清理缓存和孤儿数据
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up cache, orphaned data, and vacuum database",
	Long: `Remove LLM cache, inactive documents, orphaned content/vectors, and compact the database.

Operations performed:
  1. Delete LLM cache entries
  2. Delete inactive (soft-deleted) documents
  3. Remove orphaned content (not referenced by any document)
  4. Remove orphaned vectors (not referenced by any content)
  5. VACUUM the database to reclaim disk space`,
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}

func runCleanup(cmd *cobra.Command, args []string) error {
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	fmt.Println("Running cleanup...")

	result, err := m.GetStore().Cleanup()
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	fmt.Println()
	fmt.Printf("  LLM cache entries deleted:    %d\n", result.CacheDeleted)
	fmt.Printf("  Inactive documents deleted:   %d\n", result.InactiveDocsDeleted)
	fmt.Printf("  Orphaned content removed:     %d\n", result.OrphanedContentDeleted)
	fmt.Printf("  Orphaned vectors removed:     %d\n", result.OrphanedVectorsDeleted)
	if result.Vacuumed {
		fmt.Println("  Database vacuumed:            ✓")
	}

	total := result.CacheDeleted + result.InactiveDocsDeleted +
		result.OrphanedContentDeleted + result.OrphanedVectorsDeleted
	if total == 0 {
		fmt.Println("\nDatabase is already clean.")
	} else {
		fmt.Printf("\nCleanup complete. %d items removed.\n", total)
	}

	return nil
}
