package llm

import (
	"fmt"
	"math"
	"os"
	"sort"
	"sync"

	"github.com/hybridgroup/yzma/pkg/llama"
)

// YzmaLLM 基于 yzma (purego FFI) 的 LLM 实现
type YzmaLLM struct {
	cfg      ModelConfig
	cacheDir string
	libPath  string

	// embedding 模型
	embModel llama.Model
	embCtx   llama.Context
	embVocab llama.Vocab
	nEmbd    int32

	// generate 模型
	genModel llama.Model
	genCtx   llama.Context
	genVocab llama.Vocab

	// 模型路径
	embeddingModelPath string
	rerankModelPath    string
	generateModelPath  string

	loaded map[ModelType]bool
	mu     sync.Mutex
}

// NewYzmaLLM 创建 YzmaLLM 实例
func NewYzmaLLM(cfg ModelConfig) (*YzmaLLM, error) {
	libPath := cfg.LibPath
	if libPath == "" {
		libPath = os.Getenv("YZMA_LIB")
	}

	return &YzmaLLM{
		cfg:      cfg,
		cacheDir: cfg.CacheDir,
		libPath:  libPath,
		loaded:   make(map[ModelType]bool),
	}, nil
}

// ensureLoaded 延迟加载模型
func (y *YzmaLLM) ensureLoaded(modelType ModelType) error {
	y.mu.Lock()
	defer y.mu.Unlock()

	if y.loaded[modelType] {
		return nil
	}

	// 加载 yzma 库（首次）
	if y.libPath == "" {
		return fmt.Errorf("yzma: YZMA_LIB not set. Run 'mmq setup' or set YZMA_LIB environment variable")
	}

	// 只在第一次加载时初始化库
	if len(y.loaded) == 0 {
		if err := llama.Load(y.libPath); err != nil {
			return fmt.Errorf("yzma: failed to load library from %s: %w", y.libPath, err)
		}
		llama.Init()
		llama.LogSet(llama.LogSilent())
	}

	switch modelType {
	case ModelTypeEmbedding:
		return y.loadEmbeddingModel()
	case ModelTypeGenerate:
		return y.loadGenerateModel()
	default:
		return fmt.Errorf("yzma: unsupported model type: %s", modelType)
	}
}

// loadEmbeddingModel 加载 Embedding 模型
func (y *YzmaLLM) loadEmbeddingModel() error {
	modelPath := y.embeddingModelPath
	if modelPath == "" {
		return fmt.Errorf("yzma: embedding model path not set")
	}

	// 检查模型文件是否存在
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		if _, err2 := os.Stat(modelPath + ".gguf"); err2 == nil {
			modelPath = modelPath + ".gguf"
			y.embeddingModelPath = modelPath
		} else {
			// 自动下载
			fmt.Printf("Embedding model not found at %s, downloading...\n", modelPath)
			opts := DefaultDownloadOptions()
			if y.cacheDir != "" {
				opts.CacheDir = y.cacheDir
			}
			downloader := NewDownloader(opts)
			path, dlErr := downloader.Download(EmbeddingModelRef)
			if dlErr != nil {
				return fmt.Errorf("yzma: model not found and download failed: %w", dlErr)
			}
			modelPath = path
			y.embeddingModelPath = path
		}
	}

	// 加载模型
	model, err := llama.ModelLoadFromFile(modelPath, llama.ModelDefaultParams())
	if err != nil {
		return fmt.Errorf("yzma: failed to load embedding model %s: %w", modelPath, err)
	}

	// 配置 context（启用 embedding 模式）
	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = uint32(y.cfg.ContextSize)
	ctxParams.NBatch = uint32(y.cfg.BatchSize)
	ctxParams.PoolingType = llama.PoolingTypeMean
	ctxParams.Embeddings = 1
	if y.cfg.Threads > 0 {
		ctxParams.NThreads = int32(y.cfg.Threads)
		ctxParams.NThreadsBatch = int32(y.cfg.Threads)
	}

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		llama.ModelFree(model)
		return fmt.Errorf("yzma: failed to create embedding context: %w", err)
	}

	y.embModel = model
	y.embCtx = ctx
	y.embVocab = llama.ModelGetVocab(model)
	y.nEmbd = llama.ModelNEmbd(model)
	y.loaded[ModelTypeEmbedding] = true

	fmt.Printf("Loaded embedding model: %s (dim=%d)\n", modelPath, y.nEmbd)
	return nil
}

// loadGenerateModel 加载 Generate 模型
func (y *YzmaLLM) loadGenerateModel() error {
	modelPath := y.generateModelPath
	if modelPath == "" {
		return fmt.Errorf("yzma: generate model path not set")
	}

	// 检查模型文件是否存在
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		// 自动下载
		fmt.Printf("Generate model not found at %s, downloading...\n", modelPath)
		opts := DefaultDownloadOptions()
		if y.cacheDir != "" {
			opts.CacheDir = y.cacheDir
		}
		downloader := NewDownloader(opts)
		path, dlErr := downloader.Download(GenerateModelRef)
		if dlErr != nil {
			return fmt.Errorf("yzma: generate model not found and download failed: %w", dlErr)
		}
		modelPath = path
		y.generateModelPath = path
	}

	// 加载模型
	model, err := llama.ModelLoadFromFile(modelPath, llama.ModelDefaultParams())
	if err != nil {
		return fmt.Errorf("yzma: failed to load generate model %s: %w", modelPath, err)
	}

	// 配置 context（生成模式，较大的上下文）
	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = 2048 // 生成需要更大的上下文
	ctxParams.NBatch = uint32(y.cfg.BatchSize)
	ctxParams.Embeddings = 0 // 不需要 embedding
	if y.cfg.Threads > 0 {
		ctxParams.NThreads = int32(y.cfg.Threads)
		ctxParams.NThreadsBatch = int32(y.cfg.Threads)
	}

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		llama.ModelFree(model)
		return fmt.Errorf("yzma: failed to create generate context: %w", err)
	}

	y.genModel = model
	y.genCtx = ctx
	y.genVocab = llama.ModelGetVocab(model)
	y.loaded[ModelTypeGenerate] = true

	fmt.Printf("Loaded generate model: %s\n", modelPath)
	return nil
}

// Embed 生成文本的嵌入向量
func (y *YzmaLLM) Embed(text string, isQuery bool) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	if err := y.ensureLoaded(ModelTypeEmbedding); err != nil {
		return nil, err
	}

	y.mu.Lock()
	defer y.mu.Unlock()

	// tokenize
	tokens := llama.Tokenize(y.embVocab, text, true, true)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("yzma: tokenization produced no tokens")
	}

	// 截断到 context size
	maxTokens := int(y.cfg.ContextSize)
	if maxTokens > 0 && len(tokens) > maxTokens {
		tokens = tokens[:maxTokens]
	}

	// batch encode（embedding 模型用 Encode 而非 Decode）
	batch := llama.BatchGetOne(tokens)
	if _, err := llama.Encode(y.embCtx, batch); err != nil {
		return nil, fmt.Errorf("yzma: encode failed: %w", err)
	}

	// 获取 embedding
	vec, err := llama.GetEmbeddingsSeq(y.embCtx, 0, y.nEmbd)
	if err != nil {
		return nil, fmt.Errorf("yzma: get embeddings failed: %w", err)
	}

	// 复制一份（避免底层内存被覆盖）
	result := make([]float32, len(vec))
	copy(result, vec)

	return result, nil
}

// EmbedBatch 批量生成嵌入向量
func (y *YzmaLLM) EmbedBatch(texts []string, isQuery bool) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := y.Embed(text, isQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// Rerank 使用 embedding 余弦相似度实现重排
func (y *YzmaLLM) Rerank(query string, docs []Document) ([]RerankResult, error) {
	// 生成查询向量
	queryVec, err := y.Embed(query, true)
	if err != nil {
		return nil, fmt.Errorf("yzma rerank: failed to embed query: %w", err)
	}
	queryVec = normalizeVector(queryVec)

	results := make([]RerankResult, len(docs))
	for i, doc := range docs {
		docVec, err := y.Embed(doc.Content, false)
		if err != nil {
			return nil, fmt.Errorf("yzma rerank: failed to embed doc %d: %w", i, err)
		}
		docVec = normalizeVector(docVec)

		// 余弦相似度
		score := cosineSimilarity(queryVec, docVec)
		results[i] = RerankResult{
			ID:    doc.ID,
			Score: score,
			Index: i,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// Generate 使用 Generate 模型生成文本
// 注意：当前 yzma 的 Sampler API 存在兼容性问题，暂时返回占位结果
func (y *YzmaLLM) Generate(prompt string, opts GenerateOptions) (string, error) {
	if err := y.ensureLoaded(ModelTypeGenerate); err != nil {
		return "", err
	}

	// TODO: yzma Sampler API 存在问题，暂时返回占位结果
	// 模型已加载成功，但生成功能待完善
	return fmt.Sprintf("[Generate model loaded] Input: %s", truncateString(prompt, 100)), nil
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// ExpandQuery 使用规则进行查询扩展
// 演示 Generate 模型已加载，并提供有意义的扩展
func (y *YzmaLLM) ExpandQuery(query string) ([]QueryExpansion, error) {
	// 尝试加载 Generate 模型（演示目的）
	if err := y.ensureLoaded(ModelTypeGenerate); err != nil {
		// 模型加载失败，返回基本扩展
		return []QueryExpansion{
			{Type: "lex", Text: query, Weight: 1.0},
			{Type: "vec", Text: query, Weight: 0.9},
		}, nil
	}

	fmt.Println("Generate model loaded, creating query expansions...")

	// 基于规则的查询扩展
	expansions := []QueryExpansion{
		{Type: "lex", Text: query, Weight: 1.0},
		{Type: "vec", Text: query, Weight: 0.9},
		{Type: "hyde", Text: query, Weight: 0.7},
	}

	return expansions, nil
}

// Close 释放模型和上下文
func (y *YzmaLLM) Close() error {
	y.mu.Lock()
	defer y.mu.Unlock()

	if y.loaded[ModelTypeEmbedding] {
		llama.Free(y.embCtx)
		llama.ModelFree(y.embModel)
		y.loaded[ModelTypeEmbedding] = false
	}

	if y.loaded[ModelTypeGenerate] {
		llama.Free(y.genCtx)
		llama.ModelFree(y.genModel)
		y.loaded[ModelTypeGenerate] = false
	}

	// 只有所有模型都卸载了才关闭库
	if len(y.loaded) == 0 || (!y.loaded[ModelTypeEmbedding] && !y.loaded[ModelTypeGenerate]) {
		llama.Close()
	}

	return nil
}

// IsLoaded 检查模型是否已加载
func (y *YzmaLLM) IsLoaded(modelType ModelType) bool {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.loaded[modelType]
}

// SetModelPath 设置模型路径
func (y *YzmaLLM) SetModelPath(modelType ModelType, path string) {
	y.mu.Lock()
	defer y.mu.Unlock()

	switch modelType {
	case ModelTypeEmbedding:
		y.embeddingModelPath = path
	case ModelTypeRerank:
		y.rerankModelPath = path
	case ModelTypeGenerate:
		y.generateModelPath = path
	}
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}

	return dot / denom
}
