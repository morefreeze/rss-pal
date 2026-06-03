package api

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/extension/normalizer"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/gin-gonic/gin"
)

// extensionFeedRepo is the subset of *repository.FeedRepository the extension
// ingest handler needs. Mirrors the bookmarkletFeedRepo pattern so tests can
// swap in an in-memory stub via Go's structural typing.
//
// WithCtx returns a tx-bound view; today a no-op for public-token handlers,
// kept on the interface so handler call sites stay uniform.
type extensionFeedRepo interface {
	GetOrCreateByKindAndSource(ownerID int, feedType, sourceID, displayName string) (*model.Feed, error)
	WithCtx(c ctxkey.CtxGetter) extensionFeedRepo
}

// extensionArticleRepo is the subset of *repository.ArticleRepository the
// extension ingest handler needs.
type extensionArticleRepo interface {
	FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error)
	Create(article *model.Article) error
	WithCtx(c ctxkey.CtxGetter) extensionArticleRepo
}

// extensionUserRepo authenticates the per-user bookmarklet token, shared with
// the bookmarklet capture path. Extension ingest uses the same long-lived
// token rather than JWT so that the popup's stored token (configured once via
// the extension-config receiver) keeps working across browser sessions.
type extensionUserRepo interface {
	GetByBookmarkletToken(token string) (*model.User, error)
	WithCtx(c ctxkey.CtxGetter) extensionUserRepo
}

// ExtensionIngestHandler accepts batched per-source items from the browser
// extension, picks the matching normalizer by source_kind prefix, upserts the
// destination feed, and creates one article per item (deduping by URL).
type ExtensionIngestHandler struct {
	feedRepo    extensionFeedRepo
	articleRepo extensionArticleRepo
	userRepo    extensionUserRepo
	normalizers []normalizer.Normalizer
}

// NewExtensionIngestHandler wires the handler with concrete repositories and
// the default normalizer set (currently: TwitterNormalizer).
func NewExtensionIngestHandler(
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
	userRepo *repository.UserRepository,
) *ExtensionIngestHandler {
	return &ExtensionIngestHandler{
		feedRepo:    extensionFeedRepoAdapter{feedRepo},
		articleRepo: extensionArticleRepoAdapter{articleRepo},
		userRepo:    extensionUserRepoAdapter{userRepo},
		normalizers: []normalizer.Normalizer{
			normalizer.NewTwitterNormalizer(),
		},
	}
}

// Adapter shims wrap the concrete repositories so their WithCtx methods
// return the interface type.

type extensionFeedRepoAdapter struct{ r *repository.FeedRepository }

func (a extensionFeedRepoAdapter) GetOrCreateByKindAndSource(ownerID int, feedType, sourceID, displayName string) (*model.Feed, error) {
	return a.r.GetOrCreateByKindAndSource(ownerID, feedType, sourceID, displayName)
}
func (a extensionFeedRepoAdapter) WithCtx(c ctxkey.CtxGetter) extensionFeedRepo {
	return extensionFeedRepoAdapter{a.r.WithCtx(c)}
}

type extensionArticleRepoAdapter struct{ r *repository.ArticleRepository }

func (a extensionArticleRepoAdapter) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	return a.r.FindByOwnerAndURL(ownerID, exactURL)
}
func (a extensionArticleRepoAdapter) Create(article *model.Article) error {
	return a.r.Create(article)
}
func (a extensionArticleRepoAdapter) WithCtx(c ctxkey.CtxGetter) extensionArticleRepo {
	return extensionArticleRepoAdapter{a.r.WithCtx(c)}
}

type extensionUserRepoAdapter struct{ r *repository.UserRepository }

func (a extensionUserRepoAdapter) GetByBookmarkletToken(token string) (*model.User, error) {
	return a.r.GetByBookmarkletToken(token)
}
func (a extensionUserRepoAdapter) WithCtx(c ctxkey.CtxGetter) extensionUserRepo {
	return extensionUserRepoAdapter{a.r.WithCtx(c)}
}

// authenticate parses the Authorization: Bearer header and resolves the user
// by bookmarklet token (same scheme as BookmarkletHandler.authenticate). The
// extension and bookmarklet are both client-side capture surfaces; they share
// the per-user long-lived token so the popup's configured token works.
func (h *ExtensionIngestHandler) authenticate(c *gin.Context) (*model.User, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing token")
	}
	token := authHeader
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = authHeader[7:]
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("empty token")
	}
	user, err := h.userRepo.WithCtx(c).GetByBookmarkletToken(token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("token not found")
	}
	return user, nil
}

// ResolveOwner implements PublicTokenResolver for /api/extension/ingest.
// It reads the Authorization: Bearer header and resolves the token
// directly against the (non-RLS) users table on the open tx so the
// middleware can set app.user_id before handler logic runs.
func (h *ExtensionIngestHandler) ResolveOwner(c *gin.Context, tx *sql.Tx) (int, error) {
	token := bearerToken(c)
	if token == "" {
		return 0, ErrPublicTokenInvalid
	}
	var uid int
	err := tx.QueryRow(`SELECT id FROM users WHERE bookmarklet_token = $1`, token).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPublicTokenInvalid
	}
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// Ingest is POST /api/extension/ingest. Authenticated by bookmarklet token
// (not JWT) so it can be hit directly from the extension popup using the
// same token the user already configured for ⭐ bookmarklet capture.
func (h *ExtensionIngestHandler) Ingest(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid bookmarklet token"})
		return
	}
	userID := user.ID

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

	feed, err := h.feedRepo.WithCtx(c).GetOrCreateByKindAndSource(userID, req.SourceKind, req.SourceID, req.SourceName)
	if err != nil {
		log.Printf("extension ingest: feed upsert failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "feed upsert failed"})
		return
	}

	resp := normalizer.IngestResponse{
		FeedID:   feed.ID,
		FeedName: feed.Title,
	}
	articleRepo := h.articleRepo.WithCtx(c)
	for i, raw := range req.Items {
		art, err := norm.Normalize(raw, feed)
		if err != nil {
			resp.Errors = append(resp.Errors, "item "+strconv.Itoa(i)+": "+err.Error())
			continue
		}
		existing, _ := articleRepo.FindByOwnerAndURL(userID, art.URL)
		if existing != nil {
			resp.Skipped++
			continue
		}
		if err := articleRepo.Create(art); err != nil {
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
