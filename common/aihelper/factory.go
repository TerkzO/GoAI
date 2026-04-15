package aihelper

import (
	"context"
	"fmt"
	"sync"
)

/*
	采用工厂模式实现AI模型的创建和管理，支持动态注册和实例化多种AI模型（如OpenAI和Ollama）。
	工厂使用map存储创建者函数，确保扩展性和解耦。
*/

// ModelCreator 定义模型创建函数类型（需要 context）
type ModelCreator func(ctx context.Context, config map[string]interface{}) (AIModel, error)

// AIModelFactory AI模型工厂
type AIModelFactory struct {
	/*
		存储模型创建函数
		键为模型类型字符串（如"1"表示OpenAI，"2"表示Ollama）。
		ModelCreator定义为func(ctx context.Context, config map[string]interface{}) (AIModel, error)。
	*/
	creators map[string]ModelCreator
}

var (
	globalFactory *AIModelFactory
	factoryOnce   sync.Once
)

// GetGlobalFactory 获取全局单例
func GetGlobalFactory() *AIModelFactory {
	factoryOnce.Do(func() {
		globalFactory = &AIModelFactory{
			creators: make(map[string]ModelCreator),
		}
		globalFactory.registerCreators()
	})
	return globalFactory
}

// 注册模型
func (f *AIModelFactory) registerCreators() {
	//OpenAI
	f.creators["1"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		return NewOpenAIModel(ctx)
	}

	// 阿里百炼 RAG 模型
	f.creators["2"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		username, ok := config["username"].(string)
		if !ok {
			return nil, fmt.Errorf("RAG model requires username")
		}
		return NewAliRAGModel(ctx, username)
	}

	// MCP 模型（集成MCP服务）
	f.creators["3"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		username, ok := config["username"].(string)
		if !ok {
			return nil, fmt.Errorf("MCP model requires username")
		}
		return NewMCPModel(ctx, username)
	}

	//Ollama（目前提供接口实现，暂不提供应用，因为考虑到本地模型会占用很多空间）todo做
	f.creators["4"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		baseURL, _ := config["baseURL"].(string)
		modelName, ok := config["modelName"].(string)
		if !ok {
			return nil, fmt.Errorf("Ollama model requires modelName")
		}
		return NewOllamaModel(ctx, baseURL, modelName)
	}
	// 阿里百炼 mcp 模型
}

// CreateAIModel 根据类型创建 AI 模型
// 根据modelType从map获取创建者，调用函数实例化模型。传入context和配置参数，返回AIModel接口实例。
func (f *AIModelFactory) CreateAIModel(ctx context.Context, modelType string, config map[string]interface{}) (AIModel, error) {
	creator, ok := f.creators[modelType]
	if !ok {
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
	return creator(ctx, config)
}

// CreateAIHelper 一键创建 AIHelper
// 结合工厂和AIHelper创建，直接返回配置好的助手实例。内部调用CreateAIModel获取模型，然后NewAIHelper创建助手。
func (f *AIModelFactory) CreateAIHelper(ctx context.Context, modelType string, SessionID string, config map[string]interface{}) (*AIHelper, error) {
	model, err := f.CreateAIModel(ctx, modelType, config)
	if err != nil {
		return nil, err
	}
	return NewAIHelper(model, SessionID), nil
}

// RegisterModel 可扩展注册
// 允许运行时注册新模型类型，动态扩展支持的AI服务。全局单例工厂通过sync.Once确保线程安全初始化。
func (f *AIModelFactory) RegisterModel(modelType string, creator ModelCreator) {
	f.creators[modelType] = creator
}
