package api

import (
	"context"
	"database/sql"
	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

type shortcutContent struct{}

func (shortcutContent) FetchContent(context.Context, string) (string, error) {
	panic("shortcut must not fetch")
}
func (shortcutContent) FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error) {
	panic("shortcut must not fetch metadata")
}
func (shortcutContent) FetchHTMLDocument(context.Context, string) (*goquery.Document, error) {
	panic("shortcut must not fetch HTML")
}
func shortcutCaptureRouter(db, budgetDB *sql.DB) *gin.Engine {
	h := NewShortcutHandler(repository.NewShortcutRepository(db))
	feed := NewFeedHandler(repository.NewFeedRepository(db), repository.NewArticleRepository(db), "")
	feed.contentFetcher = shortcutContent{}
	r := gin.New()
	r.POST("/api/shortcuts/connect", h.Connect)
	policies, _ := taskbudget.LoadPolicies()
	r.POST("/api/shortcuts/capture", PublicTokenMiddleware(db, h.ResolveOwner), TaskBudgetMiddleware(taskbudget.New(budgetDB), policies), feed.CaptureFromShortcut)
	return r
}
