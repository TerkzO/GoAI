package router

import (
	"GoAI/controller/image"

	"github.com/gin-gonic/gin"
)

func ImageRouter(r *gin.RouterGroup) {
	r.POST("/image", image.RecognizeImage)
}
