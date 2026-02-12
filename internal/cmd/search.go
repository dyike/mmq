package cmd

import (
	"fmt"

	"github.com/dyike/mmq/internal/format"
	"github.com/dyike/mmq/pkg/mmq"
	"github.com/dyike/mmq/pkg/store"
	"github.com/spf13/cobra"
)

// search 命令 - BM25 全文搜索
var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "BM25 full-text search",
	Long:  "Search documents using BM25 keyword search",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

// vsearch 命令 - 向量语义搜索
var vsearchCmd = &cobra.Command{
	Use:   "vsearch <query>",
	Short: "Vector semantic search",
	Long:  "Search documents using vector similarity (requires embeddings)",
	Args:  cobra.ExactArgs(1),
	RunE:  runVSearch,
}

// query 命令 - 混合搜索 + 重排
var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Hybrid search with reranking",
	Long:  "Search using hybrid strategy (BM25 + Vector + LLM reranking) for best quality",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuery,
}

var (
	numResults int
	minScore   float64
	showAll    bool
)

func init() {
	// search 标志
	searchCmd.Flags().IntVarP(&numResults, "num", "n", 10, "Number of results")
	searchCmd.Flags().Float64Var(&minScore, "min-score", 0.0, "Minimum score threshold")
	searchCmd.Flags().BoolVar(&showAll, "all", false, "Return all matches")
	searchCmd.Flags().BoolVar(&fullContent, "full", false, "Show full content")

	// vsearch 标志
	vsearchCmd.Flags().IntVarP(&numResults, "num", "n", 10, "Number of results")
	vsearchCmd.Flags().Float64Var(&minScore, "min-score", 0.0, "Minimum score threshold")
	vsearchCmd.Flags().BoolVar(&showAll, "all", false, "Return all matches")
	vsearchCmd.Flags().BoolVar(&fullContent, "full", false, "Show full content")

	// query 标志
	queryCmd.Flags().IntVarP(&numResults, "num", "n", 10, "Number of results")
	queryCmd.Flags().Float64Var(&minScore, "min-score", 0.0, "Minimum score threshold")
	queryCmd.Flags().BoolVar(&showAll, "all", false, "Return all matches")
	queryCmd.Flags().BoolVar(&fullContent, "full", false, "Show full content")
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	limit := numResults
	if showAll {
		limit = 0 // 0 表示不限制
	}

	results, err := m.Search(query, mmq.SearchOptions{
		Limit:      limit,
		MinScore:   minScore,
		Collection: collectionFlag,
		Strategy:   mmq.StrategyFTS,
	})

	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Printf("Found %d result(s)\n\n", len(results))
	return format.OutputSearchResults(results, format.Format(outputFormat), fullContent)
}

func runVSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	// 检查索引健康状态
	if health, err := m.GetStore().CheckIndexHealth(); err == nil {
		store.PrintIndexHealthWarnings(health)
	}

	limit := numResults
	if showAll {
		limit = 0
	}

	results, err := m.Search(query, mmq.SearchOptions{
		Limit:      limit,
		MinScore:   minScore,
		Collection: collectionFlag,
		Strategy:   mmq.StrategyVector,
	})

	if err != nil {
		return fmt.Errorf("vector search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found")
		fmt.Println("Make sure documents have embeddings (run 'mmq embed')")
		return nil
	}

	fmt.Printf("Found %d result(s)\n\n", len(results))
	return format.OutputSearchResults(results, format.Format(outputFormat), fullContent)
}

func runQuery(cmd *cobra.Command, args []string) error {
	query := args[0]

	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	// 检查索引健康状态
	if health, err := m.GetStore().CheckIndexHealth(); err == nil {
		store.PrintIndexHealthWarnings(health)
	}

	limit := numResults
	if showAll {
		limit = 0
	}

	// 使用混合检索策略 + 查询扩展 + 重排
	results, err := m.Search(query, mmq.SearchOptions{
		Limit:       limit,
		MinScore:    minScore,
		Collection:  collectionFlag,
		Strategy:    mmq.StrategyHybrid,
		Rerank:      true,
		ExpandQuery: true,
	})

	if err != nil {
		return fmt.Errorf("hybrid search failed: %w", err)
	}

	// 转换为 SearchResult
	if len(results) == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Printf("Found %d result(s) using hybrid search\n\n", len(results))
	return format.OutputSearchResults(results, format.Format(outputFormat), fullContent)
}
