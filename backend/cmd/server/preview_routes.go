package main

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func registerPreviewRoutes(preview *gin.RouterGroup, explore *api.ExploreHandler, daily *api.DailyHandler, weekly *api.WeeklyHandler, briefing *api.BriefingHandler, index *api.BriefingIndexHandler) {
	registerExploreRoutes(preview, explore)
	preview.GET("/weekly-digest", weekly.Get)
	preview.GET("/daily-digest", daily.Get)
	preview.GET("/briefing/last-tab", briefing.GetLastTab)
	preview.POST("/briefing/last-tab", briefing.SetLastTab)
	preview.GET("/briefing/index", index.Get)
}
