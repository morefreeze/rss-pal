package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/youtuberelay"
	"github.com/gin-gonic/gin"
)

type YouTubeArticleSource interface {
	GetForPlayback(c *gin.Context, id, userID int) (*model.Article, error)
}

type YouTubeRelay interface {
	Start(ctx context.Context, request youtuberelay.StartRequest) (youtuberelay.Playback, error)
	Manifest(ticket string) ([]byte, error)
	Open(
		ctx context.Context,
		method string,
		ticket string,
		kind youtuberelay.StreamKind,
		rangeHeader string,
		ifRange string,
	) (*http.Response, error)
}

type YouTubePlaybackHandler struct {
	articles YouTubeArticleSource
	relay    YouTubeRelay
}

func NewYouTubePlaybackHandler(articles YouTubeArticleSource, relay YouTubeRelay) *YouTubePlaybackHandler {
	return &YouTubePlaybackHandler{articles: articles, relay: relay}
}

type repositoryYouTubeArticleSource struct {
	repo *repository.ArticleRepository
}

func NewRepositoryYouTubeArticleSource(repo *repository.ArticleRepository) YouTubeArticleSource {
	return &repositoryYouTubeArticleSource{repo: repo}
}

func (s *repositoryYouTubeArticleSource) GetForPlayback(
	c *gin.Context,
	id int,
	userID int,
) (*model.Article, error) {
	article, _, err := s.repo.WithCtx(c).GetByIDWithFeedType(id, userID)
	return article, err
}

type youtubePlaybackResponse struct {
	ManifestURL        string    `json:"manifest_url,omitempty"`
	ProgressiveURL     string    `json:"progressive_url,omitempty"`
	Mode               string    `json:"mode"`
	Quality            int       `json:"quality"`
	ProgressiveQuality int       `json:"progressive_quality,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
}

func (h *YouTubePlaybackHandler) Start(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil || articleID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	article, err := h.articles.GetForPlayback(c, articleID, getUserID(c))
	if err != nil || article == nil ||
		article.MediaType != "video/youtube" {
		c.JSON(http.StatusNotFound, gin.H{"error": "youtube article not found"})
		return
	}
	video, ok := rss.ExtractVideo(article.MediaURL)
	if !ok || video.Platform != "youtube" {
		c.JSON(http.StatusNotFound, gin.H{"error": "youtube article not found"})
		return
	}

	playback, err := h.relay.Start(c.Request.Context(), youtuberelay.StartRequest{
		UserID:    getUserID(c),
		ArticleID: articleID,
		VideoID:   video.ID,
	})
	if err != nil {
		writeYouTubeStartError(c, err)
		return
	}
	base := "/api/media/youtube/" + playback.Ticket
	response := youtubePlaybackResponse{
		Mode:               playback.Mode,
		Quality:            playback.Quality,
		ProgressiveQuality: playback.ProgressiveQuality,
		ExpiresAt:          playback.ExpiresAt,
	}
	if playback.Mode == "dash" {
		response.ManifestURL = base + "/manifest.mpd"
	}
	if playback.HasProgressive {
		response.ProgressiveURL = base + "/progressive"
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, response)
}

func (h *YouTubePlaybackHandler) Manifest(c *gin.Context) {
	body, err := h.relay.Manifest(c.Param("ticket"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "playback session not found"})
		return
	}
	c.Header("Content-Type", "application/dash+xml")
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/dash+xml", body)
}

func (h *YouTubePlaybackHandler) Media(c *gin.Context) {
	kind, ok := parseYouTubeStreamKind(c.Param("kind"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "media stream not found"})
		return
	}
	response, err := h.relay.Open(
		c.Request.Context(),
		c.Request.Method,
		c.Param("ticket"),
		kind,
		c.GetHeader("Range"),
		c.GetHeader("If-Range"),
	)
	if err != nil {
		writeYouTubeMediaError(c, err)
		return
	}
	defer response.Body.Close()

	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"ETag",
		"Last-Modified",
	} {
		for _, value := range response.Header.Values(name) {
			c.Writer.Header().Add(name, value)
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(response.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	written, copyErr := io.CopyBuffer(c.Writer, response.Body, make([]byte, 64<<10))
	if copyErr != nil {
		log.Printf("youtube_relay kind=%s status=%d bytes=%d error=%v", kind, response.StatusCode, written, copyErr)
		return
	}
	log.Printf("youtube_relay kind=%s status=%d bytes=%d", kind, response.StatusCode, written)
}

func writeYouTubeStartError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, youtuberelay.ErrCapacity):
		c.Header("Retry-After", "10")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "视频中转繁忙，请稍后重试"})
	case errors.Is(err, youtuberelay.ErrNoCompatibleMedia):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "未找到兼容的 YouTube 视频格式"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "YouTube 视频解析失败，请重试"})
	}
}

func writeYouTubeMediaError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, youtuberelay.ErrInvalidRange):
		c.Status(http.StatusRequestedRangeNotSatisfiable)
	case errors.Is(err, youtuberelay.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "playback session not found"})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "YouTube media timeout"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "YouTube media upstream failed"})
	}
}

func parseYouTubeStreamKind(raw string) (youtuberelay.StreamKind, bool) {
	switch raw {
	case string(youtuberelay.StreamVideo):
		return youtuberelay.StreamVideo, true
	case string(youtuberelay.StreamAudio):
		return youtuberelay.StreamAudio, true
	case string(youtuberelay.StreamProgressive):
		return youtuberelay.StreamProgressive, true
	default:
		return "", false
	}
}
