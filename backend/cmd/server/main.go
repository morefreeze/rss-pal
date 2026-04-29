package main

import (
	"log"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
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
	progressRepo := repository.NewProgressRepository(db)
	statsRepo := repository.NewStatsRepository(db)
	userRepo := repository.NewUserRepository(db)
	templateRepo := repository.NewTemplateRepository(db)
	shareRepo := repository.NewShareRepository(db)

	summarizer := ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
	summarizerService := service.NewSummarizerService(summarizer)

	authHandler := api.NewAuthHandler(cfg, userRepo)
	feedHandler := api.NewFeedHandler(feedRepo, articleRepo)
	articleHandler := api.NewArticleHandler(articleRepo, progressRepo, summarizerService)
	articleHandler.SetTemplateRepo(templateRepo, cfg)
	prefHandler := api.NewPreferenceHandler(prefRepo)
	progressHandler := api.NewProgressHandler(progressRepo)
	contentHandler := api.NewContentHandler(articleRepo)
	statsHandler := api.NewStatsHandler(statsRepo)
	settingsHandler := api.NewSettingsHandler(cfg, templateRepo)
	shareHandler := api.NewShareHandler(shareRepo, articleRepo)
	insightsHandler := api.NewInsightsHandler(prefRepo, templateRepo, summarizer, cfg)

	router := gin.Default()

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
		apiGroup.GET("/feeds/:id", feedHandler.GetByID)
		apiGroup.POST("/feeds", feedHandler.Create)
		apiGroup.POST("/feeds/preview", feedHandler.Preview)
		apiGroup.PUT("/feeds/:id", feedHandler.Update)
		apiGroup.DELETE("/feeds/:id", feedHandler.Delete)
		apiGroup.POST("/feeds/:id/fetch", feedHandler.FetchNow)

		// Articles
		apiGroup.GET("/articles", articleHandler.GetAll)
		apiGroup.GET("/articles/search", articleHandler.Search)
		apiGroup.GET("/articles/recommended", articleHandler.GetRecommended)
		apiGroup.GET("/articles/:id", articleHandler.GetByID)
		apiGroup.POST("/articles/:id/summary", articleHandler.GenerateSummary)
		apiGroup.POST("/articles/:id/content", contentHandler.FetchContent)
		apiGroup.GET("/articles/:id/export/md", contentHandler.ExportMarkdown)
		apiGroup.POST("/articles/:id/share", shareHandler.Create)

		// Preferences
		apiGroup.POST("/preferences/like", prefHandler.Like)
		apiGroup.POST("/preferences/dislike", prefHandler.Dislike)
		apiGroup.POST("/preferences/save", prefHandler.Save)
		apiGroup.POST("/preferences/read-duration", prefHandler.RecordReadDuration)
		apiGroup.GET("/preferences/topics", prefHandler.GetTopics)

		// Progress
		apiGroup.GET("/progress/:article_id", progressHandler.Get)
		apiGroup.POST("/progress/:article_id", progressHandler.Update)
		apiGroup.POST("/progress/:article_id/reset", progressHandler.Reset)

		// Stats
		apiGroup.GET("/stats", statsHandler.GetStats)
		apiGroup.GET("/stats/progress", statsHandler.GetProgress)

		// Insights
		apiGroup.POST("/insights/generate", insightsHandler.Generate)

		// Templates
		apiGroup.GET("/templates", settingsHandler.GetTemplates)
		apiGroup.POST("/templates", settingsHandler.CreateTemplate)
		apiGroup.DELETE("/templates/:id", settingsHandler.DeleteTemplate)

		// Settings
		apiGroup.GET("/settings/ai", settingsHandler.GetAIConfig)
		apiGroup.PUT("/settings/ai", settingsHandler.SaveAIConfig)
		apiGroup.PUT("/settings/template", settingsHandler.SetDefaultTemplate)
	}

	log.Printf("Server starting on port %s", cfg.Server.Port)
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
