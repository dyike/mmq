package llm

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// HFRef HuggingFace模型引用
type HFRef struct {
	Repo     string // 仓库名，如 "ggml-org/embeddinggemma-300M-GGUF"
	Filename string // 文件名，如 "embeddinggemma-300M-Q8_0.gguf"
	Revision string // 版本，默认为 "main"
}

// DefaultHFRef 默认模型引用
var (
	EmbeddingModelRef = HFRef{
		Repo:     "ggml-org/embeddinggemma-300M-GGUF",
		Filename: "embeddinggemma-300M-Q8_0.gguf",
		Revision: "main",
	}

	RerankModelRef = HFRef{
		Repo:     "ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF",
		Filename: "qwen3-reranker-0.6b-q8_0.gguf",
		Revision: "main",
	}

	GenerateModelRef = HFRef{
		Repo:     "ggml-org/Qwen3-0.6B-GGUF",
		Filename: "Qwen3-0.6B-Q8_0.gguf",
		Revision: "main",
	}
)

// DownloadOptions 下载选项
type DownloadOptions struct {
	CacheDir      string        // 缓存目录
	ForceDownload bool          // 强制重新下载
	Timeout       time.Duration // 超时时间
	ProgressFunc  func(downloaded, total int64)
}

// DefaultDownloadOptions 默认下载选项
func DefaultDownloadOptions() DownloadOptions {
	homeDir, _ := os.UserHomeDir()
	return DownloadOptions{
		CacheDir:      filepath.Join(homeDir, ".cache", "mmq", "models"),
		ForceDownload: false,
		Timeout:       30 * time.Minute,
	}
}

// Downloader 模型下载器
type Downloader struct {
	opts DownloadOptions
}

// NewDownloader 创建下载器
func NewDownloader(opts DownloadOptions) *Downloader {
	return &Downloader{opts: opts}
}

// Download 下载模型
func (d *Downloader) Download(ref HFRef) (string, error) {
	// 确保缓存目录存在
	if err := os.MkdirAll(d.opts.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	// 生成本地文件路径
	localPath := filepath.Join(d.opts.CacheDir, ref.Filename)

	// 检查文件是否已存在
	if !d.opts.ForceDownload {
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}

	// 构建下载URL
	url := d.buildDownloadURL(ref)

	// 下载文件
	if err := d.downloadFile(url, localPath); err != nil {
		return "", err
	}

	return localPath, nil
}

// buildDownloadURL 构建下载URL
func (d *Downloader) buildDownloadURL(ref HFRef) string {
	revision := ref.Revision
	if revision == "" {
		revision = "main"
	}

	// HuggingFace CDN URL
	return fmt.Sprintf("https://huggingface.co/%s/resolve/%s/%s",
		ref.Repo, revision, ref.Filename)
}

// downloadFile 下载文件
func (d *Downloader) downloadFile(url, localPath string) error {
	// 创建临时文件
	tmpPath := localPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: d.opts.Timeout,
	}

	// 发起下载请求
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 获取文件大小
	totalSize := resp.ContentLength

	// 创建进度跟踪器
	var downloaded int64
	progressReader := &progressReader{
		reader: resp.Body,
		onProgress: func(n int64) {
			downloaded += n
			if d.opts.ProgressFunc != nil {
				d.opts.ProgressFunc(downloaded, totalSize)
			}
		},
	}

	// 下载文件
	_, err = io.Copy(tmpFile, progressReader)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	// 关闭临时文件
	tmpFile.Close()

	// 原子性重命名
	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

// progressReader 带进度的Reader
type progressReader struct {
	reader     io.Reader
	onProgress func(n int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.onProgress(int64(n))
	}
	return n, err
}

// DownloadDefaultModels 下载默认模型
func DownloadDefaultModels(cacheDir string, progress func(model string, downloaded, total int64)) error {
	opts := DefaultDownloadOptions()
	if cacheDir != "" {
		opts.CacheDir = cacheDir
	}

	downloader := NewDownloader(opts)

	models := map[string]HFRef{
		"embedding": EmbeddingModelRef,
		"rerank":    RerankModelRef,
		"generate":  GenerateModelRef,
	}

	for name, ref := range models {
		fmt.Printf("Downloading %s model...\n", name)

		if progress != nil {
			opts.ProgressFunc = func(downloaded, total int64) {
				progress(name, downloaded, total)
			}
		}

		path, err := downloader.Download(ref)
		if err != nil {
			return fmt.Errorf("failed to download %s model: %w", name, err)
		}

		fmt.Printf("✓ %s model downloaded to: %s\n", name, path)
	}

	return nil
}

// GetModelPath 获取模型本地路径（如果已下载）
func GetModelPath(ref HFRef, cacheDir string) (string, bool) {
	if cacheDir == "" {
		homeDir, _ := os.UserHomeDir()
		cacheDir = filepath.Join(homeDir, ".cache", "mmq", "models")
	}

	localPath := filepath.Join(cacheDir, ref.Filename)

	if _, err := os.Stat(localPath); err == nil {
		return localPath, true
	}

	return localPath, false
}
