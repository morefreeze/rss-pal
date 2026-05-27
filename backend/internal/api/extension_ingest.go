package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/extension/normalizer"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

// extensionFeedRepo is the subset of *repository.FeedRepository the extension
// ingest handler needs. Mirrors the bookmarkletFeedRepo pattern so tests can
// swap in an in-memory stub via Go's structural typing.
type extensionFeedRepo interface {
	GetOrCreateByKindAndSource(ownerID int, feedType, sourceID, displayName string) (*model.Feed, error)
}

// extensionArticleRepo is the subset of *repository.ArticleRepository the
// extension ingest handler needs.
type extensionArticleRepo interface {
	FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error)
	Create(article *model.Article) error
}

// ExtensionIngestHandler accepts batched per-source items from the browser
// extension, picks the matching normalizer by source_kind prefix, upserts the
// destination feed, and creates one article per item (deduping by URL).
type ExtensionIngestHandler struct {
	feedRepo    extensionFeedRepo
	articleRepo extensionArticleRepo
	normalizers []normalizer.Normalizer
}

// NewExtensionIngestHandler wires the handler with concrete repositories and
// the default normalizer set (currently: TwitterNormalizer).
func NewExtensionIngestHandler(
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *ExtensionIngestHandler {
	return &ExtensionIngestHandler{
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
		normalizers: []normalizer.Normalizer{
			normalizer.NewTwitterNormalizer(),
		},
	}
}

// Ingest is POST /api/extension/ingest. The route is JWT-protected, so
// getUserID(c) returns the authenticated user's id (or 0 on auth failure,
// which shouldn't happen behind the middleware but we guard defensively).
func (h *ExtensionIngestHandler) Ingest(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var req normalizer.IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SourceKind == "" || req.SourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_kind and source_id required"})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusOK, normalizer.IngestResponse{})
		return
	}
	if len(req.Items) > 200 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "max 200 items per ingest"})
		return
	}

	norm := h.pickNormalizer(req.SourceKind)
	if norm == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown source_kind: " + req.SourceKind})
		return
	}

	feed, err := h.feedRepo.GetOrCreateByKindAndSource(userID, req.SourceKind, req.SourceID, req.SourceName)
	if err != nil {
		log.Printf("extension ingest: feed upsert failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "feed upsert failed"})
		return
	}

	resp := normalizer.IngestResponse{
		FeedID:   feed.ID,
		FeedName: feed.Title,
	}
	for i, raw := range req.Items {
		art, err := norm.Normalize(raw, feed)
		if err != nil {
			resp.Errors = append(resp.Errors, "item "+strconv.Itoa(i)+": "+err.Error())
			continue
		}
		existing, _ := h.articleRepo.FindByOwnerAndURL(userID, art.URL)
		if existing != nil {
			resp.Skipped++
			continue
		}
		if err := h.articleRepo.Create(art); err != nil {
			resp.Errors = append(resp.Errors, "item "+strconv.Itoa(i)+" create: "+err.Error())
			continue
		}
		resp.Accepted++
	}
	c.JSON(http.StatusOK, resp)
}

// pickNormalizer returns the first registered normalizer whose
// SourceKindPrefix matches the request's source_kind, or nil.
func (h *ExtensionIngestHandler) pickNormalizer(sourceKind string) normalizer.Normalizer {
	for _, n := range h.normalizers {
		if strings.HasPrefix(sourceKind, n.SourceKindPrefix()) {
			return n
		}
	}
	return nil
}
