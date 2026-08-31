package main

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func registerExploreRoutes(group *gin.RouterGroup, handler *api.ExploreHandler) {
	group.GET("/explore", handler.GetExplore)
	group.GET("/explore/sources", handler.GetSources)
	group.GET("/explore/articles/:id", handler.GetArticle)
	group.POST("/explore/feedback", handler.CreateFeedback)
	group.DELETE("/explore/feedback", handler.ClearNegativeFeedback)
	group.DELETE("/explore/feedback/:id", handler.DeleteFeedback)
	group.PUT("/explore/interests", handler.ReplaceInterests)
	group.POST("/explore/articles/:id/events", handler.RecordArticleEvent)
	group.POST("/explore/sources/subscribe-batch", handler.SubscribeSources)
	group.POST("/explore/sources/:id/subscribe", handler.SubscribeSource)
}
