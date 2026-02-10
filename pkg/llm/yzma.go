package llm

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
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

	loaded    map[ModelType]bool
	libLoaded bool // 标记 llama.Load() 是否已成功调用
	mu        sync.Mutex
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
	if !y.libLoaded {
		if err := llama.Load(y.libPath); err != nil {
			return fmt.Errorf("yzma: failed to load library from %s: %w", y.libPath, err)
		}
		llama.Init()
		llama.LogSet(llama.LogSilent())
		y.libLoaded = true
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

// ExpandQuery 结构化查询扩展，生成带类型的查询变体
// lex: 词法搜索变体（关键词/同义词）
// vec: 语义搜索变体（语义重述）
// hyde: 假设文档嵌入（假设性回答）
func (y *YzmaLLM) ExpandQuery(query string) ([]QueryExpansion, error) {
	// 原始查询始终作为 lex + vec 双通道，权重最高
	expansions := []QueryExpansion{
		{Type: "lex", Text: query, Weight: 2.0},
		{Type: "vec", Text: query, Weight: 2.0},
	}

	// 分词
	words := splitQueryWords(query)

	// --- lex 扩展：关键词变体，适合 BM25 ---
	// 提取有意义的关键词（去短词）
	var keywords []string
	for _, w := range words {
		if len([]rune(w)) > 2 {
			keywords = append(keywords, w)
		}
	}
	// 单独关键词作为 lex 查询
	if len(keywords) > 1 {
		for _, kw := range keywords {
			expansions = append(expansions, QueryExpansion{
				Type: "lex", Text: kw, Weight: 0.5,
			})
		}
	}
	// bigram 组合
	if len(keywords) > 2 {
		for i := 0; i < len(keywords)-1; i++ {
			bigram := keywords[i] + " " + keywords[i+1]
			expansions = append(expansions, QueryExpansion{
				Type: "lex", Text: bigram, Weight: 0.7,
			})
		}
	}

	// --- 尝试使用 LLM Generate 生成更高质量的 vec/hyde 扩展 ---
	if err := y.ensureLoaded(ModelTypeGenerate); err == nil {
		// vec 扩展：语义重述
		vecPrompt := fmt.Sprintf(
			"Rephrase this search query using different words but same meaning. "+
				"Output ONLY the rephrased query, nothing else.\nQuery: %s\nRephrased:", query)
		if vecText, err := y.Generate(vecPrompt, GenerateOptions{MaxTokens: 100}); err == nil {
			vecText = strings.TrimSpace(vecText)
			if vecText != "" && vecText != query && !strings.HasPrefix(vecText, "[") {
				expansions = append(expansions, QueryExpansion{
					Type: "vec", Text: vecText, Weight: 1.0,
				})
			}
		}

		// hyde 扩展：假设文档（生成一段假设性回答作为查询）
		hydePrompt := fmt.Sprintf(
			"Write a short paragraph (2-3 sentences) that would be a good answer to this query. "+
				"Output ONLY the paragraph, nothing else.\nQuery: %s\nAnswer:", query)
		if hydeText, err := y.Generate(hydePrompt, GenerateOptions{MaxTokens: 200}); err == nil {
			hydeText = strings.TrimSpace(hydeText)
			if hydeText != "" && !strings.HasPrefix(hydeText, "[") {
				expansions = append(expansions, QueryExpansion{
					Type: "hyde", Text: hydeText, Weight: 0.8,
				})
			}
		}
	} else {
		// LLM 不可用时的规则 vec 扩展：重排词序
		if len(words) >= 3 {
			// 反转关键词顺序作为语义变体
			reversed := make([]string, len(keywords))
			for i, w := range keywords {
				reversed[len(keywords)-1-i] = w
			}
			expansions = append(expansions, QueryExpansion{
				Type: "vec", Text: strings.Join(reversed, " "), Weight: 0.6,
			})
		}
	}

	return expansions, nil
}

// splitQueryWords 将查询分割为单词（处理中英文混合）
func splitQueryWords(text string) []string {
	var words []string
	var word []rune

	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == '?' || r == '!' {
			if len(word) > 0 {
				words = append(words, string(word))
				word = nil
			}
		} else {
			word = append(word, r)
		}
	}
	if len(word) > 0 {
		words = append(words, string(word))
	}
	return words
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

	// 只有库已加载且所有模型都卸载了才关闭库
	if y.libLoaded && !y.loaded[ModelTypeEmbedding] && !y.loaded[ModelTypeGenerate] {
		// yzma 的 BackendFree FFI 函数可能未正确注册，用 recover 保护
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("Warning: yzma library cleanup recovered from panic: %v\n", r)
				}
			}()
			llama.Close()
		}()
		y.libLoaded = false
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
