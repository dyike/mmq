//go:build !llama
// +build !llama

package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// defaultLibDir 返回默认的 yzma 库目录 ~/.cache/mmq/lib
func defaultLibDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "mmq", "lib")
}

// hasLlamaLib 检查目录中是否存在 libllama 动态库
func hasLlamaLib(dir string) bool {
	var pattern string
	switch runtime.GOOS {
	case "darwin":
		pattern = "libllama*.dylib"
	case "linux":
		pattern = "libllama*.so"
	default:
		pattern = "llama*.dll"
	}
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(matches) > 0
}

// resolveLibPath 按优先级查找 yzma 库路径
func resolveLibPath(cfgPath string) string {
	// 1. 显式配置
	if cfgPath != "" {
		return cfgPath
	}
	// 2. 环境变量
	if env := os.Getenv("YZMA_LIB"); env != "" {
		return env
	}
	// 3. 默认路径 ~/.cache/mmq/lib
	if dir := defaultLibDir(); dir != "" && hasLlamaLib(dir) {
		return dir
	}
	return ""
}

// NewLLM 创建LLM实例
// 使用 YzmaLLM（自动检测 ~/.cache/mmq/lib）
func NewLLM(cfg ModelConfig) (LLM, error) {
	libPath := resolveLibPath(cfg.LibPath)

	if libPath == "" {
		return nil, fmt.Errorf("yzma library not found. Run 'mmq setup' to download the inference library")
	}

	cfg.LibPath = libPath
	fmt.Println("Initializing YzmaLLM (local inference via yzma)")
	return NewYzmaLLM(cfg)
}
