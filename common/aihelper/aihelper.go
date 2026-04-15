package aihelper

import (
	"GoAI/common/rabbitmq"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"sync"
)

// AIHelper AI助手结构体，包含消息历史和AI模型
// AIHelper :
// 结构封装消息历史、
// AI 模型实例
// 特定于会话的上下文
type AIHelper struct {
	model     AIModel                                      // AI模型接口，支持不同模型实现
	messages  []*model.Message                             // 消息历史列表，存储用户和AI的对话记录
	mu        sync.RWMutex                                 // 读写锁，保护消息历史并发访问
	SessionID string                                       // 会话唯一标识，用于绑定消息和上下文
	saveFunc  func(*model.Message) (*model.Message, error) // 消息存储回调函数，默认异步发布到RabbitMQ
}

// NewAIHelper 创建新的AIHelper实例.设置默认saveFunc为RabbitMQ发布。消息列表为空切片，SessionID从参数传入。
func NewAIHelper(model_ AIModel, SessionID string) *AIHelper {
	return &AIHelper{
		model:    model_,
		messages: make([]*model.Message, 0),
		//异步推送到消息队列中 RabbitMQ 的默认异步持久化策略
		saveFunc: func(msg *model.Message) (*model.Message, error) {
			data := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UserName, msg.IsUser)
			err := rabbitmq.RMQMessage.Publish(data)
			return msg, err
		},
		SessionID: SessionID,
	}
}

// addMessage 添加新消息到历史，自动调用saveFunc持久化。若Save参数为false，仅内存存储。使用锁保护并发安全。
func (a *AIHelper) AddMessage(Content string, UserName string, IsUser bool, Save bool) {
	userMsg := model.Message{
		SessionID: a.SessionID,
		Content:   Content,
		UserName:  UserName,
		IsUser:    IsUser,
	}
	a.messages = append(a.messages, &userMsg)
	if Save {
		a.saveFunc(&userMsg)
	}
}

// SaveMessage 保存消息到数据库（通过回调函数避免循环依赖）
// 通过传入func，自己调用外部的保存函数，即可支持同步异步等多种策略
func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

// GetMessages 获取所有消息历史; 避免外部修改。使用读锁确保线程安全。
func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// 同步生成 构建消息上下文，调用模型接口生成回复。用户消息先添加历史，AI回复后存储。
func (a *AIHelper) GenerateResponse(userName string, ctx context.Context, userQuestion string) (*model.Message, error) {

	//调用存储函数
	a.AddMessage(userQuestion, userName, true, true)

	a.mu.RLock()
	//将model.Message转化成schema.Message
	messages := utils.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	//调用模型生成回复
	schemaMsg, err := a.model.GenerateResponse(ctx, messages)
	if err != nil {
		return nil, err
	}

	//将schema.Message转化成model.Message
	modelMsg := utils.ConvertToModelMessage(a.SessionID, userName, schemaMsg)

	//调用存储函数
	a.AddMessage(modelMsg.Content, userName, false, true)

	return modelMsg, nil
}

// 流式生成 流式模式通过回调实时输出。
func (a *AIHelper) StreamResponse(userName string, ctx context.Context, cb StreamCallback, userQuestion string) (*model.Message, error) {

	//调用存储函数
	a.AddMessage(userQuestion, userName, true, true)

	a.mu.RLock()
	messages := utils.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	content, err := a.model.StreamResponse(ctx, messages, cb)
	if err != nil {
		return nil, err
	}
	//转化成model.Message
	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UserName:  userName,
		Content:   content,
		IsUser:    false,
	}

	//调用存储函数
	a.AddMessage(modelMsg.Content, userName, false, true)

	return modelMsg, nil
}

// GetModelType 获取模型类型
func (a *AIHelper) GetModelType() string {
	return a.model.GetModelType()
}
