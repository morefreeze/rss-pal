package main

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func registerInterestRoutes(routes gin.IRoutes, handler *api.InterestsHandler) {
	routes.GET("/interests/latest", handler.Latest)
	routes.POST("/interests/generate", handler.Generate)
	routes.GET("/insights/latest", handler.LatestLegacy)
	routes.POST("/insights/generate", handler.Generate)
}
