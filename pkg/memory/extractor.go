package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dyike/mmq/pkg/llm"
)

// ExtractedMemory 从对话中提取的记忆
type ExtractedMemory struct {
	Type    string `json:"type"`              // fact, preference, important
	Content string `json:"content"`           // 记忆内容
	Subject string `json:"subject,omitempty"` // 事实主体（可选）
}

// Extractor 从对话中自动提取记忆
type Extractor struct {
	apiClient *llm.APIClient
	manager   *Manager
}

// NewExtractor 创建记忆提取器
func NewExtractor(apiClient *llm.APIClient, manager *Manager) *Extractor {
	return &Extractor{
		apiClient: apiClient,
		manager:   manager,
	}
}

// extractionPrompt 提取记忆的 prompt
const extractionPrompt = `分析以下对话，提取用户明确**陈述**的**持久性**事实或偏好。

严格规则：
1. 只提取用户**主动告知**的信息（如 "我叫张三"、"我喜欢Python"、"我是程序员"）
2. 用户**提问**不是陈述！"我叫什么？"、"你知道我的名字吗？" 这类问句不包含任何事实，不要提取
3. 不要从助手的回答中提取任何信息，助手可能会猜错
4. 不要推测、不要脑补，只提取对话中**字面明确出现**的用户自述信息
5. **排除临时状态**：如"我有点闲"、"我现在很累"、"今天心情不好"等即时状态不是持久事实，不要提取
6. type 只能是: fact（事实）或 preference（偏好）
7. 如果没有可提取的信息，必须返回空数组 []

应该提取（持久信息）：
- "我叫张三" → [{"type":"fact","content":"用户的名字是张三"}] ✅
- "我喜欢看科幻电影" → [{"type":"preference","content":"用户喜欢看科幻电影"}] ✅
- "我是做后端开发的" → [{"type":"fact","content":"用户从事后端开发工作"}] ✅

不应提取：
- "我叫什么名字？" → [] ❌ 提问不是陈述
- "我有点闲" → [] ❌ 临时状态
- "今天好累" → [] ❌ 临时状态
- 助手说"你叫mmq" → [] ❌ 不从助手回答提取

对话：
%s

返回 JSON 数组（无其他文字）：`

// ExtractFromTurn 从单轮对话中提取记忆并存储
func (e *Extractor) ExtractFromTurn(turn ConversationTurn) (int, error) {
	if e.apiClient == nil {
		return 0, nil
	}

	convText := fmt.Sprintf("用户: %s\n助手: %s", turn.User, turn.Assistant)

	// 过滤太短的对话
	if len([]rune(turn.User)) < 5 {
		return 0, nil
	}

	// 调用 LLM 提取
	prompt := fmt.Sprintf(extractionPrompt, convText)
	messages := []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}

	response, err := e.apiClient.Chat(messages, 0.0, 300)
	if err != nil {
		return 0, fmt.Errorf("extraction failed: %w", err)
	}

	extracted := parseExtractionResponse(response)
	if len(extracted) == 0 {
		return 0, nil
	}

	// 存储（带去重）
	return e.storeWithDedup(extracted, turn.SessionID)
}

// ExtractFromHistory 从多轮对话中提取记忆
func (e *Extractor) ExtractFromHistory(turns []ConversationTurn) (int, error) {
	if e.apiClient == nil || len(turns) == 0 {
		return 0, nil
	}

	var lines []string
	for _, turn := range turns {
		lines = append(lines, fmt.Sprintf("用户: %s\n助手: %s", turn.User, turn.Assistant))
	}
	convText := strings.Join(lines, "\n---\n")
	if len(convText) > 3000 {
		convText = string([]rune(convText)[:3000])
	}

	prompt := fmt.Sprintf(extractionPrompt, convText)
	messages := []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}

	response, err := e.apiClient.Chat(messages, 0.0, 300)
	if err != nil {
		return 0, fmt.Errorf("extraction failed: %w", err)
	}

	extracted := parseExtractionResponse(response)
	if len(extracted) == 0 {
		return 0, nil
	}

	sessionID := ""
	if len(turns) > 0 {
		sessionID = turns[0].SessionID
	}
	return e.storeWithDedup(extracted, sessionID)
}

// storeWithDedup 存储提取到的记忆（跳过重复项）
func (e *Extractor) storeWithDedup(extracted []ExtractedMemory, sessionID string) (int, error) {
	// 获取现有事实和偏好用于去重
	existingFacts, _ := e.manager.GetByType(MemoryTypeFact)
	existingPrefs, _ := e.manager.GetByType(MemoryTypePreference)
	var existingContents []string
	for _, m := range existingFacts {
		existingContents = append(existingContents, strings.ToLower(m.Content))
	}
	for _, m := range existingPrefs {
		existingContents = append(existingContents, strings.ToLower(m.Content))
	}

	stored := 0
	for _, mem := range extracted {
		// 去重：检查是否已存在相似内容
		if isDuplicate(mem.Content, existingContents) {
			continue
		}

		var memType MemoryType
		switch mem.Type {
		case "fact", "important":
			memType = MemoryTypeFact
		case "preference":
			memType = MemoryTypePreference
		default:
			memType = MemoryTypeFact
		}

		metadata := map[string]interface{}{
			"source": "auto_extract",
		}
		if sessionID != "" {
			metadata["session_id"] = sessionID
		}
		if mem.Subject != "" {
			metadata["subject"] = mem.Subject
		}

		m := Memory{
			Type:       memType,
			Content:    mem.Content,
			Metadata:   metadata,
			Tags:       []string{"auto"},
			Timestamp:  time.Now(),
			Importance: 0.7,
		}

		if err := e.manager.Store(m); err != nil {
			continue
		}

		// 将新内容加入已有列表防止本轮内重复
		existingContents = append(existingContents, strings.ToLower(mem.Content))
		stored++
	}

	return stored, nil
}

// isDuplicate 检查内容是否与已有记忆重复
func isDuplicate(newContent string, existingContents []string) bool {
	newLower := strings.ToLower(strings.TrimSpace(newContent))
	if newLower == "" {
		return true
	}

	for _, existing := range existingContents {
		// 完全相同
		if newLower == existing {
			return true
		}
		// 一个包含另一个（如 "用户的名字是张三" vs "用户名字是张三"）
		if strings.Contains(newLower, existing) || strings.Contains(existing, newLower) {
			return true
		}
		// 去除标点后比较核心内容
		newCore := stripPunctuation(newLower)
		existCore := stripPunctuation(existing)
		if newCore == existCore {
			return true
		}
	}
	return false
}

// stripPunctuation 去除标点和空白
func stripPunctuation(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff { // 中文字符
			b.WriteRune(r)
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseExtractionResponse 解析 LLM 提取结果
func parseExtractionResponse(response string) []ExtractedMemory {
	response = strings.TrimSpace(response)

	// 处理 markdown 代码块包裹
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	// 尝试找到 JSON 数组
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start >= 0 && end > start {
		response = response[start : end+1]
	}

	var extracted []ExtractedMemory
	if err := json.Unmarshal([]byte(response), &extracted); err != nil {
		return nil
	}

	return extracted
}
