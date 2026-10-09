package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type redditBrowserStore interface {
	RegisterSubreddit(context.Context, string) (string, error)
	ListSubreddits(context.Context) ([]repository.RedditSubreddit, error)
	RemoveSubreddit(context.Context, string) error
	Ingest(context.Context, explore.RedditBrowserBatch, time.Time) (repository.RedditBrowserResult, error)
}
type RedditBrowserHandler struct {
	auth  *ExtensionIngestHandler
	store redditBrowserStore
}

func NewRedditBrowserHandler(auth *ExtensionIngestHandler, store redditBrowserStore) *RedditBrowserHandler {
	return &RedditBrowserHandler{auth: auth, store: store}
}

func (h *RedditBrowserHandler) Ingest(c *gin.Context) {
	user, err := h.auth.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid bookmarklet token"})
		return
	}
	if !user.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "administrator required for shared discovery"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	var batch explore.RedditBrowserBatch
	if err := c.ShouldBindJSON(&batch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or oversized Reddit batch"})
		return
	}
	now := time.Now().UTC()
	// Validate untrusted uploads before opening a database transaction. The
	// repository checks the actual provider threshold again under the row lock.
	if _, _, err := batch.Parse(explore.Provider{Endpoint: explore.RedditTopEndpoint(batch.Subreddit, batch.Period, 100)}, now); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.store.Ingest(c.Request.Context(), batch, now)
	if errors.Is(err, repository.ErrRedditBrowserDisabled) {
		c.JSON(http.StatusConflict, gin.H{"error": "Reddit provider is disabled or not configured for browser collection"})
		return
	}
	if err != nil {
		log.Printf("Reddit browser discovery ingest failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "discovery ingest failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// RegisterSubreddit is separate from ingest: merely visiting a Reddit page or
// uploading an unregistered community cannot silently expand shared discovery.
func (h *RedditBrowserHandler) RegisterSubreddit(c *gin.Context) {
	user, err := h.auth.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid bookmarklet token"})
		return
	}
	if !user.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "administrator required for shared discovery"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var body struct {
		Subreddit string `json:"subreddit"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subreddit request"})
		return
	}
	name, err := explore.NormalizeSubreddit(body.Subreddit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err = h.store.RegisterSubreddit(c.Request.Context(), name)
	if errors.Is(err, repository.ErrRedditBrowserDisabled) {
		c.JSON(http.StatusConflict, gin.H{"error": "Reddit provider is disabled or unavailable"})
		return
	}
	if err != nil {
		log.Printf("register Reddit subreddit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "subreddit registration failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subreddit": name})
}

func (h *RedditBrowserHandler) ListSubreddits(c *gin.Context) {
	user, err := h.auth.authenticate(c)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid bookmarklet token"})
		return
	}
	if !user.IsAdmin {
		c.JSON(403, gin.H{"error": "administrator required"})
		return
	}
	h.ListManagedSubreddits(c)
}
func (h *RedditBrowserHandler) RemoveSubreddit(c *gin.Context) {
	user, err := h.auth.authenticate(c)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid bookmarklet token"})
		return
	}
	if !user.IsAdmin {
		c.JSON(403, gin.H{"error": "administrator required"})
		return
	}
	h.RemoveManagedSubreddit(c)
}

// Managed handlers are mounted behind JWT and administrator middleware.
func (h *RedditBrowserHandler) ListManagedSubreddits(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.store.ListSubreddits(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "无法加载 subreddit 列表"})
		return
	}
	c.JSON(200, items)
}
func (h *RedditBrowserHandler) RemoveManagedSubreddit(c *gin.Context) {
	name, err := explore.NormalizeSubreddit(c.Param("name"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid subreddit"})
		return
	}
	if err = h.store.RemoveSubreddit(c.Request.Context(), name); err != nil {
		c.JSON(500, gin.H{"error": "移除失败"})
		return
	}
	c.Status(204)
}
