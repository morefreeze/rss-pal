package main

import (
	"log"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	prefRepo := repository.NewPreferenceRepository(db)
	playbackRepo := repository.NewPlaybackProgressRepository(db)
	progressRepo := repository.NewProgressRepository(db)
	statsRepo := repository.NewStatsRepository(db)
	userRepo := repository.NewUserRepository(db)
	templateRepo := repository.NewTemplateRepository(db)
	shareRepo := repository.NewShareRepository(db)
	recommendedRepo := repository.NewRecommendedFeedRepository(db)
	weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)
	eventRepo := repository.NewEventRepository(db)
	feedHealthRepo := repository.NewFeedHealthRepository(db)
	userTagRepo := repository.NewUserTagRepository(db)
	articleUserTagRepo := repository.NewArticleUserTagRepository(db)
	tagSuggestRepo := repository.NewTagSuggestionRepository(db)
	savedRepo := repository.NewSavedRepository(db)

	summarizer := ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
	summarizerService := service.NewSummarizerService(summarizer)

	backupRunner := backup.NewRunner(db, cfg.Backup.Dir)

	contentFetcher := rss.NewContentFetcher()

	authHandler := api.NewAuthHandler(cfg, userRepo)
	feedHandler := api.NewFeedHandler(feedRepo, articleRepo, cfg.RSSHub.BaseURL).WithBackupRunner(backupRunner)
	adminHandler := api.NewAdminHandler(db, backupRunner, cfg)
	articleHandler := api.NewArticleHandler(articleRepo, articleUserTagRepo, progressRepo, prefRepo, summarizerService, contentFetcher)
	articleHandler.SetTemplateRepo(templateRepo, cfg)
	prefHandler := api.NewPreferenceHandler(prefRepo, articleRepo)
	progressHandler := api.NewProgressHandler(progressRepo, eventRepo)
	rssFetcher := rss.NewFetcher(cfg.RSSHub.BaseURL)
	contentHandler := api.NewContentHandler(articleRepo, feedRepo, rssFetcher)
	statsHandler := api.NewStatsHandler(statsRepo)
	settingsHandler := api.NewSettingsHandler(cfg, templateRepo, userRepo)
	shareHandler := api.NewShareHandler(shareRepo, articleRepo)
	userInsightsRepo := repository.NewUserInsightRepository(db)
	insightsHandler := api.NewInsightsHandler(prefRepo, articleRepo, templateRepo, userInsightsRepo, summarizer, cfg)
	recommendedHandler := api.NewRecommendedHandler(recommendedRepo, feedRepo)
	weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo, summarizer)
	bookmarkletHandler := api.NewBookmarkletHandler(userRepo, feedRepo, articleRepo)
	playbackHandler := api.NewPlaybackHandler(playbackRepo, prefRepo)
	eventHandler := api.NewEventHandler(eventRepo)
	feedHealthHandler := api.NewFeedHealthHandler(feedHealthRepo, feedRepo)
	userTagHandler := api.NewUserTagHandler(userTagRepo, articleUserTagRepo, tagSuggestRepo)
	savedHandler := api.NewSavedHandler(savedRepo, articleUserTagRepo)

	router := gin.Default()
	// Trust only requests from localhost/private networks (running behind nginx)
	router.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})

	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Public routes
	router.POST("/api/auth/init", authHandler.InitAdmin)
	router.POST("/api/auth/login", authHandler.Login)
	router.POST("/api/auth/register", authHandler.Register)

	// Public share route (no auth required)
	router.GET("/api/share/:token", shareHandler.GetByToken)

	// Public image proxy (no auth — <img> tags can't reliably carry auth headers).
	router.GET("/api/proxy/image", api.NewImageProxy().Handle)

	// Public bookmarklet capture (CORS + per-user token auth, no JWT)
	router.POST("/api/bookmarklet/capture", bookmarkletHandler.Capture)

	// Protected routes
	apiGroup := router.Group("/api")
	apiGroup.Use(authHandler.AuthMiddleware())
	{
		// User
		apiGroup.GET("/auth/me", authHandler.GetMe)
		apiGroup.PUT("/auth/password", authHandler.ChangePassword)
		apiGroup.POST("/auth/invite-codes", authHandler.CreateInviteCode)
		apiGroup.GET("/auth/invite-codes", authHandler.ListInviteCodes)

		// Feeds
		apiGroup.GET("/feeds", feedHandler.GetAll)
		apiGroup.GET("/feeds/export/opml", feedHandler.ExportOPML)
		apiGroup.GET("/feeds/:id", feedHandler.GetByID)
		apiGroup.POST("/feeds", feedHandler.Create)
		apiGroup.POST("/feeds/preview", feedHandler.Preview)
		apiGroup.POST("/feeds/oneoff_link_set", feedHandler.CreateOneoffLinkSet)
		apiGroup.PUT("/feeds/:id", feedHandler.Update)
		apiGroup.DELETE("/feeds/:id", feedHandler.Delete)
		apiGroup.POST("/feeds/:id/fetch", feedHandler.FetchNow)
		apiGroup.PATCH("/feeds/:id/status", feedHandler.UpdateStatus)
		apiGroup.PATCH("/feeds/:id/weight", feedHandler.UpdateWeight)
		apiGroup.GET("/feeds/health", feedHealthHandler.Get)

		// Manual tags
		apiGroup.GET("/tags", userTagHandler.ListTags)
		apiGroup.POST("/tags", userTagHandler.CreateTag)
		// /tags/sidebar must be before /tags/:id so Gin doesn't match :id=sidebar
		apiGroup.GET("/tags/sidebar", userTagHandler.GetTagSidebar)
		apiGroup.PATCH("/tags/:id", userTagHandler.RenameTag)
		apiGroup.DELETE("/tags/:id", userTagHandler.DeleteTag)

		// Articles
		apiGroup.GET("/articles", articleHandler.GetAll)
		apiGroup.GET("/articles/grouped", articleHandler.GetGrouped)
		apiGroup.GET("/articles/search", articleHandler.Search)
		apiGroup.GET("/articles/recommended/link_set", articleHandler.GetLinkSetRecommended)
		apiGroup.GET("/articles/recommended", articleHandler.GetRecommended)
		apiGroup.GET("/articles/unread-count", articleHandler.GetUnreadCount)
		apiGroup.POST("/articles/mark-all-read", articleHandler.MarkAllRead)
		apiGroup.GET("/articles/:id", articleHandler.GetByID)
		apiGroup.POST("/articles/:id/summary", articleHandler.GenerateSummary)
		apiGroup.POST("/articles/:id/content", contentHandler.FetchContent)
		apiGroup.GET("/articles/:id/export/md", contentHandler.ExportMarkdown)
		apiGroup.POST("/articles/:id/share", shareHandler.Create)
		apiGroup.GET("/articles/:id/playback", playbackHandler.Get)
		apiGroup.PUT("/articles/:id/playback", playbackHandler.Put)
		apiGroup.GET("/articles/:id/tags", userTagHandler.GetArticleTags)
		apiGroup.POST("/articles/:id/tags", userTagHandler.AddArticleTag)
		apiGroup.DELETE("/articles/:id/tags/:tagId", userTagHandler.RemoveArticleTag)
		apiGroup.POST("/articles/:id/suggestions/dismiss", userTagHandler.DismissSuggestion)
		apiGroup.POST("/articles/:id/expand", articleHandler.ExpandChild)
		apiGroup.GET("/articles/:id/candidates", articleHandler.GetCandidates)
		apiGroup.POST("/articles/:id/batch_fetch", articleHandler.BatchFetch)

		// Saved articles (filtered by tags / source / untagged)
		apiGroup.GET("/saved", savedHandler.List)

		// Preferences
		apiGroup.POST("/preferences/like", prefHandler.Like)
		apiGroup.POST("/preferences/dislike", prefHandler.Dislike)
		apiGroup.POST("/preferences/save", prefHandler.Save)
		apiGroup.DELETE("/preferences/save", prefHandler.Unsave)
		apiGroup.POST("/preferences/read-duration", prefHandler.RecordReadDuration)
		apiGroup.GET("/preferences/topics", prefHandler.GetTopics)
		apiGroup.GET("/preferences/tags", prefHandler.GetTags)
		apiGroup.DELETE("/preferences/topics/:id", prefHandler.DeleteTopic)
		apiGroup.DELETE("/preferences/tags/:id", prefHandler.DeleteTag)

		// Progress
		apiGroup.GET("/progress/:article_id", progressHandler.Get)
		apiGroup.POST("/progress/:article_id", progressHandler.Update)
		apiGroup.POST("/progress/:article_id/reset", progressHandler.Reset)

		// Behavioral events (exposure/click)
		apiGroup.POST("/events", eventHandler.Create)

		// Stats
		apiGroup.GET("/stats", statsHandler.GetStats)
		apiGroup.GET("/stats/progress", statsHandler.GetProgress)

		// Insights
		apiGroup.GET("/insights/latest", insightsHandler.Latest)
		apiGroup.POST("/insights/generate", insightsHandler.Generate)

		// Recommended feeds (catalog)
		apiGroup.GET("/recommended-feeds", recommendedHandler.List)
		apiGroup.POST("/recommended-feeds/:id/subscribe", recommendedHandler.Subscribe)

		// Weekly digest
		apiGroup.GET("/weekly-digest", weeklyHandler.Get)

		// Templates
		apiGroup.GET("/templates", settingsHandler.GetTemplates)
		apiGroup.POST("/templates", settingsHandler.CreateTemplate)
		apiGroup.DELETE("/templates/:id", settingsHandler.DeleteTemplate)

		// Settings
		apiGroup.GET("/settings/ai", settingsHandler.GetAIConfig)
		apiGroup.PUT("/settings/ai", settingsHandler.SaveAIConfig)
		apiGroup.PUT("/settings/template", settingsHandler.SetDefaultTemplate)
		apiGroup.POST("/settings/polish-prompt", settingsHandler.PolishPrompt)
		apiGroup.GET("/settings/bookmarklet-token", settingsHandler.GetBookmarkletToken)
		apiGroup.POST("/settings/bookmarklet-token/regenerate", settingsHandler.RegenerateBookmarkletToken)

		// Admin: subscription backup + restore (host-mounted /backups dir)
		apiGroup.GET("/admin/backups", adminHandler.ListBackups)
		apiGroup.POST("/admin/backups", adminHandler.CreateBackupNow)
		apiGroup.POST("/admin/backups/restore", adminHandler.RestoreBackup)
	}

	log.Printf("Server starting on port %s", cfg.Server.Port)
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
