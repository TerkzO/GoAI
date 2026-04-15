package aihelper

import (
	"context"
	"sync"
)

// 采用单例模式管理用户-会话-AIHelper的映射关系，实现实例缓存和生命周期控制。

var ctx = context.Background()

// AIHelperManager AI助手管理器，管理用户-会话-AIHelper的映射关系
type AIHelperManager struct {
	helpers map[string]map[string]*AIHelper // 外层map键为用户名，内层map键为会话ID，值为AIHelper指针。支持多用户多会话隔离。
	mu      sync.RWMutex
}

// NewAIHelperManager 创建新的管理器实例
func NewAIHelperManager() *AIHelperManager {
	return &AIHelperManager{
		helpers: make(map[string]map[string]*AIHelper),
	}
}

// 检查是否存在助手，若无则通过工厂创建并存储。传入用户名、会话ID、模型类型和配置，确保实例唯一性。
func (m *AIHelperManager) GetOrCreateAIHelper(userName string, sessionID string, modelType string, config map[string]interface{}) (*AIHelper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取用户的会话映射
	userHelpers, exists := m.helpers[userName]
	if !exists {
		userHelpers = make(map[string]*AIHelper)
		m.helpers[userName] = userHelpers
	}

	// 检查会话是否已存在
	helper, exists := userHelpers[sessionID]
	if exists {
		return helper, nil
	}

	// 创建新的AIHelper
	// 使用sync.Once返回单例实例，提供统一管理入口。
	factory := GetGlobalFactory()
	helper, err := factory.CreateAIHelper(ctx, modelType, sessionID, config)
	if err != nil {
		return nil, err
	}

	userHelpers[sessionID] = helper
	return helper, nil
}

// 获取指定用户的指定会话的AIHelper
func (m *AIHelperManager) GetAIHelper(userName string, sessionID string) (*AIHelper, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return nil, false
	}

	helper, exists := userHelpers[sessionID]
	return helper, exists
}

// 移除指定用户的指定会话的AIHelper
// 删除指定助手，GetAIHelper获取现有实例，GetUserSessions返回用户的所有会话ID列表。
func (m *AIHelperManager) RemoveAIHelper(userName string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return
	}

	delete(userHelpers, sessionID)

	// 如果用户没有会话了，清理用户映射
	if len(userHelpers) == 0 {
		delete(m.helpers, userName)
	}
}

// 获取指定用户的所有会话ID
func (m *AIHelperManager) GetUserSessions(userName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return []string{}
	}

	sessionIDs := make([]string, 0, len(userHelpers))
	//取出所有的key
	for sessionID := range userHelpers {
		sessionIDs = append(sessionIDs, sessionID)
	}

	return sessionIDs
}

// 全局管理器实例
var globalManager *AIHelperManager
var once sync.Once

// GetGlobalManager 获取全局管理器实例
func GetGlobalManager() *AIHelperManager {
	once.Do(func() {
		globalManager = NewAIHelperManager()
	})
	return globalManager
}
