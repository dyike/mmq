package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dyike/mmq/pkg/mmq"
	"github.com/spf13/cobra"
)

var (
	// DefaultDBPath 默认数据库路径
	DefaultDBPath string

	// Version 版本号
	Version string

	// BuildTime 构建时间
	BuildTime string

	// 全局标志
	dbPath         string
	collectionFlag string
	outputFormat   string
)

// printUsageTree 从 cobra 命令树自动生成usage
func printUsageTree(root *cobra.Command) {
	var lines []string
	maxLen := 0

	// 收集所有命令行
	var collect func(cmd *cobra.Command, prefix string)
	collect = func(cmd *cobra.Command, prefix string) {
		for _, sub := range cmd.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.HasSubCommands() {
				collect(sub, prefix+sub.Name()+" ")
			} else {
				use := prefix + sub.Use
				if len(use) > maxLen {
					maxLen = len(use)
				}
				lines = append(lines, use+"\t"+sub.Short)
			}
		}
	}
	collect(root, root.Name()+" ")

	// 对齐输出
	fmt.Println("Usage:")
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		padding := maxLen - len(parts[0]) + 2
		if padding < 2 {
			padding = 2
		}
		fmt.Printf("  %s%s- %s\n", parts[0], strings.Repeat(" ", padding), parts[1])
	}
}

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:     "mmq",
	Short:   "Model Memory & Query - RAG and memory management",
	Version: Version,
	Run: func(cmd *cobra.Command, args []string) {
		printUsageTree(cmd)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 全局标志
	rootCmd.PersistentFlags().StringVarP(&dbPath, "db", "d", DefaultDBPath, "Database path")
	rootCmd.PersistentFlags().StringVarP(&collectionFlag, "collection", "c", "", "Collection filter")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "format", "f", "text", "Output format (text|json|csv|md|xml)")

	// 添加子命令
	rootCmd.AddCommand(collectionCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(multiGetCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(embedCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(vsearchCmd)
	rootCmd.AddCommand(queryCmd)

	// 版本模板
	rootCmd.SetVersionTemplate(fmt.Sprintf("mmq version %s (built %s)\n", Version, BuildTime))

}

// getMMQ 获取MMQ实例（辅助函数）
func getMMQ() (*mmq.MMQ, error) {
	// 确保数据库目录存在
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	m, err := mmq.NewWithDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return m, nil
}
