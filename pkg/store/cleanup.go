package store

import "fmt"

// CleanupResult 清理操作结果
type CleanupResult struct {
	CacheDeleted           int  // 清除的缓存条目数
	InactiveDocsDeleted    int  // 删除的非活跃文档数
	OrphanedContentDeleted int  // 清理的孤儿内容数
	OrphanedVectorsDeleted int  // 清理的孤儿向量数
	Vacuumed               bool // 是否已压缩
}

// Cleanup 执行所有清理操作
func (s *Store) Cleanup() (*CleanupResult, error) {
	result := &CleanupResult{}

	// 1. 清除 LLM 缓存
	count, err := s.deleteLLMCache()
	if err != nil {
		return nil, fmt.Errorf("delete LLM cache: %w", err)
	}
	result.CacheDeleted = count

	// 2. 删除非活跃文档
	count, err = s.deleteInactiveDocuments()
	if err != nil {
		return nil, fmt.Errorf("delete inactive documents: %w", err)
	}
	result.InactiveDocsDeleted = count

	// 3. 清理孤儿内容（不被任何文档引用的 content）
	count, err = s.cleanupOrphanedContent()
	if err != nil {
		return nil, fmt.Errorf("cleanup orphaned content: %w", err)
	}
	result.OrphanedContentDeleted = count

	// 4. 清理孤儿向量（不被任何 content 引用的 vectors）
	count, err = s.cleanupOrphanedVectors()
	if err != nil {
		return nil, fmt.Errorf("cleanup orphaned vectors: %w", err)
	}
	result.OrphanedVectorsDeleted = count

	// 5. 压缩数据库
	if err := s.vacuum(); err != nil {
		return nil, fmt.Errorf("vacuum database: %w", err)
	}
	result.Vacuumed = true

	return result, nil
}

// deleteLLMCache 清除所有 LLM 缓存
func (s *Store) deleteLLMCache() (int, error) {
	res, err := s.db.Exec("DELETE FROM llm_cache")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// deleteInactiveDocuments 删除 active=0 的文档
func (s *Store) deleteInactiveDocuments() (int, error) {
	res, err := s.db.Exec("DELETE FROM documents WHERE active = 0")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// cleanupOrphanedContent 清理不被任何 document 引用的 content
func (s *Store) cleanupOrphanedContent() (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM content
		WHERE hash NOT IN (SELECT DISTINCT hash FROM documents)
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// cleanupOrphanedVectors 清理不被任何 content 引用的向量
func (s *Store) cleanupOrphanedVectors() (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM content_vectors
		WHERE hash NOT IN (SELECT DISTINCT hash FROM content)
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// vacuum 压缩数据库文件
func (s *Store) vacuum() error {
	_, err := s.db.Exec("VACUUM")
	return err
}
