package jwt

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/utils/myjwt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

/*
	中间件认证
	路由中，未认证接口（如注册、登录）无需中间件;
	需认证接口（如 AI 聊天）应用中间件。
*/

// 读取jwt
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		res := new(controller.Response)

		var token string
		authHeader := c.GetHeader("Authorization")
		// 检查 Authorization Header（Bearer 格式）或 URL 参数 token。
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 兼容URL参数传token
			token = c.Query("token")
		}

		// 解析 Token，验证有效性。
		if token == "" {
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		log.Println("token is ", token)
		userName, ok := myjwt.ParseToken(token)
		if !ok {
			// 失败时返回错误并中止请求。
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}
		// 将用户名存储到 Gin 上下文 (c.Set("userName", userName))。
		c.Set("userName", userName)
		c.Next()
	}
}
