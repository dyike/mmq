package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dyike/mmq/pkg/llm"
	"github.com/spf13/cobra"
)

var expandCmd = &cobra.Command{
	Use:   "expand <query>",
	Short: "Expand query using LLM",
	Long: `Expand a query into multiple variants using the Generate model.

This uses the LLM to generate query expansions for better retrieval:
  - lex: Lexical variants (synonyms, related terms)
  - vec: Semantic variants (rephrased for vector search)
  - hyde: Hypothetical document (what a relevant doc might look like)

Example:
  mmq expand "如何使用 Go 语言"
  mmq expand "machine learning basics" --json`,
	Args: cobra.ExactArgs(1),
	RunE: runExpand,
}

var expandJSON bool

func init() {
	rootCmd.AddCommand(expandCmd)
	expandCmd.Flags().BoolVar(&expandJSON, "json", false, "Output as JSON")
}

func runExpand(cmd *cobra.Command, args []string) error {
	query := args[0]

	// 获取模型缓存目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	modelsDir := filepath.Join(homeDir, ".cache", "mmq", "models")

	// 检查生成模型是否存在
	generatePath, exists := llm.GetModelPath(llm.GenerateModelRef, modelsDir)
	if !exists {
		return fmt.Errorf("Generate model not found. Run 'mmq setup' first")
	}

	// 创建 LLM 实例
	config := llm.DefaultModelConfig()
	config.CacheDir = modelsDir
	config.LibPath = os.Getenv("YZMA_LIB")

	llmImpl, err := llm.NewLLM(config)
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}
	defer llmImpl.Close()

	// 设置生成模型路径
	llmImpl.SetModelPath(llm.ModelTypeGenerate, generatePath)

	fmt.Printf("Expanding query: %s\n\n", query)

	// 调用查询扩展
	expansions, err := llmImpl.ExpandQuery(query)
	if err != nil {
		return fmt.Errorf("failed to expand query: %w", err)
	}

	if expandJSON {
		// JSON 输出
		data, err := json.MarshalIndent(expansions, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	} else {
		// 文本输出
		fmt.Printf("Found %d expansions:\n\n", len(expansions))
		for i, exp := range expansions {
			fmt.Printf("[%d] Type: %s (weight: %.2f)\n", i+1, exp.Type, exp.Weight)
			fmt.Printf("    Text: %s\n\n", exp.Text)
		}
	}

	return nil
}
