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
	"github.com/bytedance/rss-pal/internal/version"
	gingzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	adminDB, err := repository.NewAdminDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to admin database: %v", err)
	}
	defer adminDB.Close()

	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	articleRepo.SetImageBaseDir(cfg.Backup.Dir)
	prefRepo := repository.NewPreferenceRepository(db)
	playbackRepo := repository.NewPlaybackProgressRepository(db)
	progressRepo := repository.NewProgressRepository(db)
	statsRepo := repository.NewStatsRepository(db)
	userRepo := repository.NewUserRepository(db)
	templateRepo := repository.NewTemplateRepository(db)
	shareRepo := repository.NewShareRepository(db)
	weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)
	dailyDigestRepo := repository.NewDailyDigestRepository(db)
	eventRepo := repository.NewEventRepository(db)
	feedHealthRepo := repository.NewFeedHealthRepository(db)
	userTagRepo := repository.NewUserTagRepository(db)
	articleUserTagRepo := repository.NewArticleUserTagRepository(db)
	tagSuggestRepo := repository.NewTagSuggestionRepository(db)
	clipRepo := repository.NewClipRepository(db)
	hiddenRepo := repository.NewHiddenArticleRepository(db)

	summarizer := ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
	summarizer.SetVisionModel(cfg.AI.Vision.Model)
	summarizerService := service.NewSummarizerService(summarizer)

	backupRunner := backup.NewRunner(adminDB, cfg.Backup.Dir)

	contentFetcher := rss.NewContentFetcher()

	authHandler := api.NewAuthHandler(cfg, userRepo)
	feedHandler := api.NewFeedHandler(feedRepo, articleRepo, cfg.RSSHub.BaseURL).WithBackupRunner(backupRunner)
	adminHandler := api.NewAdminHandler(adminDB, backupRunner, cfg)
	articleHandler := api.NewArticleHandler(articleRepo, articleUserTagRepo, progressRepo, prefRepo, hiddenRepo, summarizerService, contentFetcher)
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
	weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo)
	dailyHandler := api.NewDailyHandler(articleRepo, dailyDigestRepo)
	briefingHandler := api.NewBriefingHandler(userRepo)
	briefingIndexHandler := api.NewBriefingIndexHandler(dailyDigestRepo, weeklyDigestRepo)
	bookmarkletHandler := api.NewBookmarkletHandler(userRepo, feedRepo, articleRepo).
		WithBackupRunner(backupRunner).
		WithImageBaseDir(cfg.Backup.Dir)
	playbackHandler := api.NewPlaybackHandler(playbackRepo, prefRepo)
	eventHandler := api.NewEventHandler(eventRepo)
	feedHealthHandler := api.NewFeedHealthHandler(feedHealthRepo, feedRepo)
	userTagHandler := api.NewUserTagHandler(userTagRepo, articleUserTagRepo, tagSuggestRepo)
	clipHandler := api.NewClipHandler(clipRepo, articleUserTagRepo)
	extensionIngestHandler := api.NewExtensionIngestHandler(feedRepo, articleRepo, userRepo)

	router := gin.Default()
	// Compress JSON/text responses for clients that opt in. Defensive
	// when the API is reached directly (no nginx); skip already-compressed
	// content types and the streaming summary endpoint that controls its
	// own framing.
	router.Use(gingzip.Gzip(
		gingzip.DefaultCompression,
		gingzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".mp4", ".mp3", ".woff", ".woff2"}),
		gingzip.WithExcludedPathsRegexs([]string{"/api/articles/.*/summary/stream"}),
	))
	// Trust only requests from localhost/private networks (running behind nginx)
	router.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})

	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "X-New-Token")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Health check (public, no auth)
	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "version": version.Version})
	})

	// Public routes
	router.POST("/api/auth/init", authHandler.InitAdmin)
	router.POST("/api/auth/login", authHandler.Login)
	router.POST("/api/auth/register", authHandler.Register)

	// Public share route — no JWT, but PublicTokenMiddleware opens a tx and
	// sets app.user_id to the share token's creator so RLS-protected reads
	// (articles, feeds) see the owner's rows. Without this wrap the handler
	// would silently return empty rows after migration 033.
	router.GET("/api/share/:token",
		api.PublicTokenMiddleware(db, shareHandler.ResolveOwner),
		shareHandler.GetByToken)

	// Public image proxy (no auth — <img> tags can't reliably carry auth headers).
	router.GET("/api/proxy/image", api.NewImageProxy().Handle)

	// PDF clip images. Public for the same <img>-tag-can't-carry-Authorization
	// reason as /api/proxy/image. The URL itself is the access token:
	// <articleID, idx> is hard to enumerate meaningfully, and the only thing
	// behind the URL is an extracted figure raster (never source content,
	// never DB rows). Acceptable trade-off for a personal single-user tool;
	// signed-URL tokens would be the next step if multi-tenant.
	//
	// Task 4.2 note: this endpoint is INTENTIONALLY NOT wrapped in
	// PublicTokenMiddleware because the handler reads from the file system,
	// not the DB — no RLS surface area. If a future change makes it query
	// articles/feeds, switch the closure to a resolver that opens the tx,
	// sets app.bypass_rls LOCAL for the article→feed→owner_id chase, then
	// returns the owner_id so the middleware can set app.user_id.
	pdfImgHandler := api.NewArticleImageHandler(cfg.Backup.Dir,
		func(c *gin.Context, articleID int) (bool, error) { return true, nil })
	router.GET("/api/articles/:id/images/:idx", pdfImgHandler.Serve)

	// Public bookmarklet capture (CORS + per-user token auth, no JWT).
	// PublicTokenMiddleware resolves the owning user from the bearer
	// bookmarklet_token, opens a per-request tx, and sets app.user_id so
	// every repository.WithCtx call inside the handler runs under RLS.
	router.POST("/api/bookmarklet/capture",
		api.PublicTokenMiddleware(db, bookmarkletHandler.ResolveOwner),
		bookmarkletHandler.Capture)
	// PDF capture variants share the same per-user bookmarklet token auth.
	// capture-pdf takes multipart form-data with the PDF bytes from the
	// browser; capture-pdf-url asks the server to fetch the PDF itself.
	router.POST("/api/bookmarklet/capture-pdf",
		api.PublicTokenMiddleware(db, bookmarkletHandler.ResolveOwner),
		bookmarkletHandler.CapturePDF)
	router.POST("/api/bookmarklet/capture-pdf-url",
		api.PublicTokenMiddleware(db, bookmarkletHandler.ResolveOwner),
		bookmarkletHandler.CapturePDFURL)

	// Extension ingest uses the same per-user bookmarklet token as the
	// bookmarklet capture path (not JWT), so the popup's configured token
	// works for both ⭐ capture-html and ⚡ adapter-driven ingest.
	router.POST("/api/extension/ingest",
		api.PublicTokenMiddleware(db, extensionIngestHandler.ResolveOwner),
		extensionIngestHandler.Ingest)

	// Protected routes
	apiGroup := router.Group("/api")
	apiGroup.Use(authHandler.AuthMiddleware())
	apiGroup.Use(api.RLSTxMiddleware(db))
	{
		// User
		apiGroup.GET("/auth/me", authHandler.GetMe)
		apiGroup.PUT("/auth/password", authHandler.ChangePassword)
		apiGroup.PUT("/auth/visibility-floor", authHandler.UpdateVisibilityFloor)
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
		apiGroup.POST("/articles/:id/confirm_link_set", articleHandler.ConfirmLinkSetSuggestion)
		apiGroup.POST("/articles/:id/hide", articleHandler.Hide)
		apiGroup.DELETE("/articles/:id/hide", articleHandler.Unhide)

		// (PDF clip image route is registered above as a public endpoint —
		// <img> tags can't carry Bearer tokens, so JWT-gated wouldn't render.)

		// Clip articles (filtered by tags / source / untagged)
		apiGroup.GET("/clip", clipHandler.List)

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

		// Weekly / daily briefings (worker generates async; API is read-only)
		apiGroup.GET("/weekly-digest", weeklyHandler.Get)
		apiGroup.GET("/daily-digest", dailyHandler.Get)
		apiGroup.GET("/briefing/last-tab", briefingHandler.GetLastTab)
		apiGroup.POST("/briefing/last-tab", briefingHandler.SetLastTab)
		apiGroup.GET("/briefing/index", briefingIndexHandler.Get)

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
		apiGroup.POST("/admin/backups/restore-upload", adminHandler.RestoreBackupUpload)
		apiGroup.GET("/admin/backups/download/:name", adminHandler.DownloadBackup)
	}

	log.Printf("Server starting on port %s", cfg.Server.Port)
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
