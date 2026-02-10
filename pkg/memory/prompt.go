package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/dyike/mmq/pkg/rag"
)

// PromptBuilder 记忆感知的 Prompt 组装器
type PromptBuilder struct {
	manager   *Manager
	recencyK  int // 最近 K 轮对话
	factTopK  int // 最相关的 K 条事实
	maxMemLen int // 记忆部分最大字符数
}

// NewPromptBuilder 创建 PromptBuilder
func NewPromptBuilder(manager *Manager) *PromptBuilder {
	return &PromptBuilder{
		manager:   manager,
		recencyK:  5,
		factTopK:  10,
		maxMemLen: 2000,
	}
}

// SetRecencyK 设置最近对话轮数
func (b *PromptBuilder) SetRecencyK(k int) { b.recencyK = k }

// SetFactTopK 设置相关事实数量
func (b *PromptBuilder) SetFactTopK(k int) { b.factTopK = k }

// BuildSystemPrompt 组装包含记忆的 system prompt
func (b *PromptBuilder) BuildSystemPrompt(sessionID string, userQuery string, ragContexts []rag.Context) string {
	var parts []string

	parts = append(parts, `你是一个通用智能助手。请根据用户的实际问题来回答。
注意事项：
- 如果用户没有告诉你他的名字，你不知道他叫什么，请如实回答"我不知道"
- 下方的"记忆"和"文档"仅供参考，不要从中推断用户的身份信息
- 只在用户问题与文档内容相关时才引用文档，否则正常对话即可`)

	// 1. 对话历史
	if sessionID != "" {
		convMem := NewConversationMemory(b.manager)
		history, err := convMem.GetHistory(sessionID, b.recencyK)
		if err == nil && len(history) > 0 {
			var convLines []string
			for _, turn := range history {
				convLines = append(convLines, fmt.Sprintf("用户: %s\n助手: %s", turn.User, turn.Assistant))
			}
			historyText := strings.Join(convLines, "\n---\n")
			if len(historyText) > b.maxMemLen/2 {
				historyText = historyText[:b.maxMemLen/2] + "..."
			}
			parts = append(parts, fmt.Sprintf("\n[对话记忆（最近%d轮）]\n%s", len(history), historyText))
		}
	}

	// 2. 相关事实
	if userQuery != "" {
		factMem := NewFactMemory(b.manager)
		facts, err := factMem.SearchFacts(userQuery, b.factTopK)
		if err == nil && len(facts) > 0 {
			var factLines []string
			for _, f := range facts {
				factLines = append(factLines, fmt.Sprintf("- %s %s %s", f.Subject, f.Predicate, f.Object))
			}
			parts = append(parts, fmt.Sprintf("\n[已知事实]\n%s", strings.Join(factLines, "\n")))
		}
	}

	// 3. 用户偏好
	prefMem := NewPreferenceMemory(b.manager)
	allPrefs, err := prefMem.GetAllPreferences()
	if err == nil && len(allPrefs) > 0 {
		var prefLines []string
		for cat, kvs := range allPrefs {
			for k, v := range kvs {
				prefLines = append(prefLines, fmt.Sprintf("- %s.%s = %v", cat, k, v))
			}
		}
		if len(prefLines) > 0 {
			parts = append(parts, fmt.Sprintf("\n[用户偏好]\n%s", strings.Join(prefLines, "\n")))
		}
	}

	// 4. 通用记忆召回（补充事实和偏好以外的记忆）
	if userQuery != "" {
		memories, err := b.manager.Recall(userQuery, RecallOptions{
			Limit:              5,
			MemoryTypes:        nil, // 所有类型
			ApplyDecay:         true,
			DecayHalflife:      30 * 24 * time.Hour,
			WeightByImportance: true,
			MinRelevance:       0.3,
		})
		if err == nil && len(memories) > 0 {
			var memLines []string
			for _, mem := range memories {
				memLines = append(memLines, fmt.Sprintf("- [%s] %s", mem.Type, truncateStr(mem.Content, 100)))
			}
			parts = append(parts, fmt.Sprintf("\n[相关记忆]\n%s", strings.Join(memLines, "\n")))
		}
	}

	// 5. RAG 文档上下文（仅注入高相关度的）
	if len(ragContexts) > 0 {
		var ragLines []string
		for i, ctx := range ragContexts {
			// 过滤低相关度的文档，避免注入噪声
			if ctx.Relevance < 0.3 {
				continue
			}
			snippet := truncateStr(ctx.Text, 500)
			ragLines = append(ragLines, fmt.Sprintf("[%d] (来源: %s, 相关度: %.2f)\n%s", i+1, ctx.Source, ctx.Relevance, snippet))
		}
		if len(ragLines) > 0 {
			parts = append(parts, fmt.Sprintf("\n[参考文档（仅在与用户问题相关时引用）]\n%s", strings.Join(ragLines, "\n\n")))
		}
	}

	return strings.Join(parts, "\n")
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return strings.ReplaceAll(s, "\n", " ")
	}
	return strings.ReplaceAll(string(runes[:maxLen]), "\n", " ") + "..."
}
