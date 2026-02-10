package store

import (
	"fmt"
	"time"
)

// IndexHealth 索引健康状态
type IndexHealth struct {
	TotalDocuments   int    // 总文档数
	MissingEmbedding int    // 缺少嵌入的文档数
	HasVectorIndex   bool   // 是否有向量索引表
	OldestUpdateDays int    // 距最后更新天数（-1 表示无文档）
	OldestUpdateDate string // 最后更新时间
}

// CheckIndexHealth 检查索引健康状态
func (s *Store) CheckIndexHealth() (*IndexHealth, error) {
	h := &IndexHealth{OldestUpdateDays: -1}

	// 1. 总活跃文档数
	s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE active = 1`).Scan(&h.TotalDocuments)

	// 2. 检查向量索引表是否存在
	var tableName string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='vectors_vec'`).Scan(&tableName)
	h.HasVectorIndex = (err == nil)

	// 3. 缺少嵌入的文档数（有文档但没有对应的 content_vectors 记录）
	s.db.QueryRow(`
		SELECT COUNT(DISTINCT d.hash)
		FROM documents d
		LEFT JOIN content_vectors cv ON cv.hash = d.hash
		WHERE d.active = 1 AND cv.hash IS NULL
	`).Scan(&h.MissingEmbedding)

	// 4. 距最后更新的天数
	var oldestDate string
	err = s.db.QueryRow(`
		SELECT MAX(modified_at) FROM documents WHERE active = 1
	`).Scan(&oldestDate)
	if err == nil && oldestDate != "" {
		h.OldestUpdateDate = oldestDate
		if t, err := time.Parse(time.RFC3339, oldestDate); err == nil {
			h.OldestUpdateDays = int(time.Since(t).Hours() / 24)
		}
	}

	return h, nil
}

// PrintIndexHealthWarnings 打印索引健康警告到 stderr
func PrintIndexHealthWarnings(h *IndexHealth) {
	if h.TotalDocuments == 0 {
		return
	}

	if !h.HasVectorIndex {
		fmt.Println("⚠ Vector index not found. Run 'mmq embed' to create embeddings.")
		return
	}

	if h.MissingEmbedding > 0 {
		pct := h.MissingEmbedding * 100 / h.TotalDocuments
		fmt.Printf("⚠ %d/%d documents (%d%%) missing embeddings. Run 'mmq embed' to update.\n",
			h.MissingEmbedding, h.TotalDocuments, pct)
	}

	if h.OldestUpdateDays > 7 {
		fmt.Printf("⚠ Index last updated %d days ago (%s). Run 'mmq update' to refresh.\n",
			h.OldestUpdateDays, h.OldestUpdateDate[:10])
	}
}
