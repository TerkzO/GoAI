package router

import (
	"GoAI/controller/session"
	"GoAI/controller/tts"

	"github.com/gin-gonic/gin"
)

func AIRouter(r *gin.RouterGroup) {

	// 聊天相关接口
	{
		r.GET("/chat/sessions", session.GetUserSessionsByUserName)            // 获取用户会话列表
		r.POST("/chat/send-new-session", session.CreateSessionAndSendMessage) // 创建新会话并发送消息（同步）
		r.POST("/chat/send", session.ChatSend)                                // 向现有会话发送消息（同步）。
		r.POST("/chat/history", session.ChatHistory)                          // 获取会话历史。Controller调用service.GetChatHistory，AIHelper返回内存消息历史。
		// r.POST("/chat/tts", AI.ChatSpeech)                  // ChatSpeechHandler
		r.POST("/chat/send-stream-new-session", session.CreateStreamSessionAndSendMessage) // 创建新会话并流式发送消息。
		r.POST("/chat/send-stream", session.ChatStreamSend)                                // 向现有会话流式发送消息。

		// TTS相关接口
		r.POST("/chat/tts", tts.CreateTTSTask)
		r.GET("/chat/tts/query", tts.QueryTTSTask)
	}
}
