## 消息交互流程
前端                    路由           认证       session_controller.go controller      session_service.go service
AIChat.vue聊天界面 -> AI.go/chat/* -> jwt.go ->  ChatSend/ChatStreamSend等          -> CreateSessionAndSendMessage等

     实例管理              模型创建          模型交互(生成回复)  异步存储
 |-> AIHelperManager -> AIModelFactory -> AIHelper  ->     RabbitMQ
 |                                              |
 |                                              v
 |                                       AI API(OpenAI/ollama)提供模型推理
-|-> redis缓存状态
 |
 |    session_dao.go Dao
 |->  CreateSession
      message.go 消息操作

消息通过RabbitMQ异步持久化，避免阻塞。流式模式用SSE推送实时内容
