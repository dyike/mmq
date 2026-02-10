package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dyike/mmq/pkg/llm"
	"github.com/dyike/mmq/pkg/memory"
	"github.com/dyike/mmq/pkg/rag"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	chatSession  string
	chatNoMemory bool
	chatNoRAG    bool
	chatModel    string
)

var chatCmd = &cobra.Command{
	Use:   "chat [message]",
	Short: "Chat with LLM (with memory and RAG)",
	Long: `Start an interactive chat session with LLM, enhanced with memory recall and document retrieval.

Supports external APIs via environment variables:
  DEEPSEEK_API_KEY    → Deepseek (default)
  OPENAI_API_KEY      → OpenAI
  (no key)            → Ollama local (http://localhost:11434)

Examples:
  mmq chat                           # 交互式模式
  mmq chat "A股今天怎么样"             # 单轮问答
  mmq chat --session my-session      # 恢复指定会话
  mmq chat --no-memory "你好"         # 不使用记忆`,
	RunE: runChat,
}

func init() {
	chatCmd.Flags().StringVar(&chatSession, "session", "", "Session ID (default: auto-generated)")
	chatCmd.Flags().BoolVar(&chatNoMemory, "no-memory", false, "Disable memory injection")
	chatCmd.Flags().BoolVar(&chatNoRAG, "no-rag", false, "Disable RAG context retrieval")
	chatCmd.Flags().StringVar(&chatModel, "model", "", "Override model name")
}

func runChat(cmd *cobra.Command, args []string) error {
	// 1. 初始化 MMQ
	m, err := getMMQ()
	if err != nil {
		return err
	}
	defer m.Close()

	// 2. 初始化 API 客户端
	apiClient := llm.NewAPIClient()
	if chatModel != "" {
		apiClient.Model = chatModel
	}

	if !apiClient.IsConfigured() {
		fmt.Println("⚠️  未配置 API Key，将尝试连接本地 Ollama")
		fmt.Println("   设置环境变量 DEEPSEEK_API_KEY 或 OPENAI_API_KEY 来使用云端 API")
	}

	fmt.Printf("🤖 MMQ Chat (provider: %s, model: %s)\n", apiClient.Provider(), apiClient.Model)

	// 3. 会话管理
	sessionID := chatSession
	if sessionID == "" {
		sessionID = uuid.New().String()[:8]
	}
	fmt.Printf("📝 Session: %s\n", sessionID)

	// 4. 准备记忆和 RAG 组件
	mgr := m.GetMemoryManager()
	promptBuilder := memory.NewPromptBuilder(mgr)
	convMem := memory.NewConversationMemory(mgr)
	extractor := memory.NewExtractor(apiClient, mgr)

	// 构建 RAG retriever
	var retriever *rag.Retriever
	if !chatNoRAG {
		retriever = rag.NewRetriever(m.GetStore(), m.GetLLM(), m.GetEmbedding())
	}

	// 5. 维护对话消息历史（用于发送给 API）
	var messages []llm.ChatMessage

	// 单轮模式
	if len(args) > 0 {
		userMsg := strings.Join(args, " ")
		return chatOnce(apiClient, promptBuilder, convMem, extractor, retriever, messages, sessionID, userMsg)
	}

	// 6. 交互式 REPL
	fmt.Println("💬 输入消息开始对话 (输入 /quit 退出, /help 查看命令)")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("你: ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// 处理斜杠命令
		if strings.HasPrefix(input, "/") {
			if handleSlashCmd(input, convMem, sessionID, &messages) {
				break // /quit
			}
			continue
		}

		// 构建 system prompt（含记忆）
		var ragContexts []rag.Context
		if retriever != nil && !chatNoRAG && shouldUseRAG(input) {
			ragContexts, _ = retriever.Retrieve(input, rag.RetrieveOptions{
				Limit:       3,
				Strategy:    rag.StrategyHybrid,
				ExpandQuery: false,
			})
		}

		var systemPrompt string
		if !chatNoMemory {
			systemPrompt = promptBuilder.BuildSystemPrompt(sessionID, input, ragContexts)
		} else {
			systemPrompt = "你是一个智能助手。"
			if len(ragContexts) > 0 {
				systemPrompt += "\n\n[相关文档]\n"
				for i, ctx := range ragContexts {
					systemPrompt += fmt.Sprintf("[%d] %s\n", i+1, truncateForChat(ctx.Text, 500))
				}
			}
		}

		// 组装消息
		apiMessages := []llm.ChatMessage{
			{Role: "system", Content: systemPrompt},
		}
		// 添加对话历史
		apiMessages = append(apiMessages, messages...)
		// 添加当前用户消息
		apiMessages = append(apiMessages, llm.ChatMessage{Role: "user", Content: input})

		// 流式输出
		fmt.Print("\n🤖: ")
		reply, err := apiClient.ChatStream(apiMessages, 0.7, 4096, func(chunk string) {
			fmt.Print(chunk)
		})
		fmt.Println()
		fmt.Println()

		if err != nil {
			fmt.Printf("❌ API 错误: %v\n\n", err)
			continue
		}

		// 更新消息历史
		messages = append(messages,
			llm.ChatMessage{Role: "user", Content: input},
			llm.ChatMessage{Role: "assistant", Content: reply},
		)

		// 保持最近 10 轮对话在上下文中
		if len(messages) > 20 {
			messages = messages[len(messages)-20:]
		}

		// 存储对话轮次到记忆
		if !chatNoMemory {
			turn := memory.ConversationTurn{
				User:      input,
				Assistant: reply,
				SessionID: sessionID,
				Timestamp: time.Now(),
			}
			_ = convMem.StoreTurn(turn)

			// 自动提取记忆（后台执行，不阻塞对话）
			go func() {
				if n, err := extractor.ExtractFromTurn(turn); err == nil && n > 0 {
					fmt.Fprintf(os.Stderr, "[记忆] 自动提取了 %d 条新记忆\n", n)
				}
			}()
		}
	}

	fmt.Println("\n👋 再见!")
	return nil
}

// chatOnce 单轮问答模式
func chatOnce(
	apiClient *llm.APIClient,
	promptBuilder *memory.PromptBuilder,
	convMem *memory.ConversationMemory,
	extractor *memory.Extractor,
	retriever *rag.Retriever,
	messages []llm.ChatMessage,
	sessionID, userMsg string,
) error {
	// RAG 检索（仅对内容相关的查询）
	var ragContexts []rag.Context
	if retriever != nil && shouldUseRAG(userMsg) {
		ragContexts, _ = retriever.Retrieve(userMsg, rag.RetrieveOptions{
			Limit:       3,
			Strategy:    rag.StrategyHybrid,
			ExpandQuery: false,
		})
	}

	// 构建 prompt
	var systemPrompt string
	if !chatNoMemory {
		systemPrompt = promptBuilder.BuildSystemPrompt(sessionID, userMsg, ragContexts)
	} else {
		systemPrompt = "你是一个智能助手。"
	}

	apiMessages := []llm.ChatMessage{
		{Role: "system", Content: systemPrompt},
	}
	apiMessages = append(apiMessages, messages...)
	apiMessages = append(apiMessages, llm.ChatMessage{Role: "user", Content: userMsg})

	// 流式输出
	reply, err := apiClient.ChatStream(apiMessages, 0.7, 4096, func(chunk string) {
		fmt.Print(chunk)
	})
	fmt.Println()

	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}

	// 存储对话 + 自动提取
	if !chatNoMemory {
		turn := memory.ConversationTurn{
			User:      userMsg,
			Assistant: reply,
			SessionID: sessionID,
			Timestamp: time.Now(),
		}
		_ = convMem.StoreTurn(turn)
		if n, _ := extractor.ExtractFromTurn(turn); n > 0 {
			fmt.Fprintf(os.Stderr, "[记忆] 自动提取了 %d 条新记忆\n", n)
		}
	}

	return nil
}

// handleSlashCmd 处理斜杠命令，返回 true 表示退出
func handleSlashCmd(input string, convMem *memory.ConversationMemory, sessionID string, messages *[]llm.ChatMessage) bool {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/quit", "/exit", "/q":
		return true

	case "/help", "/h":
		fmt.Println("Available commands:")
		fmt.Println("  /quit, /q        退出")
		fmt.Println("  /clear           清除当前对话上下文")
		fmt.Println("  /history         查看当前会话历史")
		fmt.Println("  /sessions        查看所有会话")
		fmt.Println("  /memory          切换记忆开关")
		fmt.Println("  /rag             切换 RAG 开关")
		fmt.Println()

	case "/clear":
		*messages = nil
		fmt.Println("✓ 对话上下文已清除")
		fmt.Println()

	case "/history":
		turns, err := convMem.GetHistory(sessionID, 10)
		if err != nil || len(turns) == 0 {
			fmt.Println("暂无历史对话")
		} else {
			fmt.Printf("── 会话 %s 历史 (%d轮) ──\n", sessionID, len(turns))
			for _, turn := range turns {
				fmt.Printf("  [%s] 你: %s\n", turn.Timestamp.Format("15:04"), truncateForChat(turn.User, 60))
				fmt.Printf("         🤖: %s\n", truncateForChat(turn.Assistant, 60))
			}
		}
		fmt.Println()

	case "/sessions":
		sessions, err := convMem.GetSessionIDs()
		if err != nil || len(sessions) == 0 {
			fmt.Println("暂无会话")
		} else {
			fmt.Printf("所有会话 (%d):\n", len(sessions))
			for _, s := range sessions {
				count, _ := convMem.CountBySession(s)
				marker := ""
				if s == sessionID {
					marker = " ← 当前"
				}
				fmt.Printf("  %s (%d轮)%s\n", s, count, marker)
			}
		}
		fmt.Println()

	case "/memory":
		chatNoMemory = !chatNoMemory
		if chatNoMemory {
			fmt.Println("📴 记忆已关闭")
		} else {
			fmt.Println("📡 记忆已开启")
		}
		fmt.Println()

	case "/rag":
		chatNoRAG = !chatNoRAG
		if chatNoRAG {
			fmt.Println("📴 RAG 已关闭")
		} else {
			fmt.Println("📡 RAG 已开启")
		}
		fmt.Println()

	default:
		fmt.Printf("未知命令: %s (输入 /help 查看)\n\n", cmd)
	}

	return false
}

func truncateForChat(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// shouldUseRAG 判断是否需要触发 RAG 文档检索
// 闲聊、打招呼、个人问题等不需要检索文档
func shouldUseRAG(query string) bool {
	q := strings.TrimSpace(query)
	runes := []rune(q)

	// 太短的查询（< 5 个字符/汉字）通常是闲聊
	if len(runes) < 5 {
		return false
	}

	// 常见闲聊/个人问题关键词
	casualPatterns := []string{
		"你好", "你是谁", "你叫什么", "我叫什么", "我是谁",
		"名字", "你能做什么", "帮我", "谢谢", "再见",
		"hello", "hi ", "hey", "who are you", "what's your name",
		"what is your name", "my name",
	}
	qLower := strings.ToLower(q)
	for _, p := range casualPatterns {
		if strings.Contains(qLower, p) {
			return false
		}
	}

	return true
}
