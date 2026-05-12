package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
)

type ArticleHandler struct {
	articleRepo    *repository.ArticleRepository
	progressRepo   *repository.ProgressRepository
	prefRepo       *repository.PreferenceRepository
	summarizer     *service.SummarizerService
	templateRepo   *repository.TemplateRepository
	cfg            *config.Config
	contentFetcher *rss.ContentFetcher
}

func (h *ArticleHandler) GetUnreadCount(c *gin.Context) {
	count, err := h.articleRepo.GetUnreadCount(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

func (h *ArticleHandler) MarkAllRead(c *gin.Context) {
	var feedID *int
	if fid := c.Query("feed_id"); fid != "" {
		if id, err := strconv.Atoi(fid); err == nil {
			feedID = &id
		}
	}
	unreadOnly := c.Query("unread") == "true"
	savedOnly := c.Query("saved") == "true"

	if err := h.progressRepo.MarkAllRead(getUserID(c), feedID, unreadOnly, savedOnly); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已全部标记为已读"})
}

func NewArticleHandler(articleRepo *repository.ArticleRepository, progressRepo *repository.ProgressRepository, prefRepo *repository.PreferenceRepository, summarizer *service.SummarizerService, contentFetcher *rss.ContentFetcher) *ArticleHandler {
	return &ArticleHandler{
		articleRepo:    articleRepo,
		progressRepo:   progressRepo,
		prefRepo:       prefRepo,
		summarizer:     summarizer,
		contentFetcher: contentFetcher,
	}
}

// SetTemplateRepo allows injecting templateRepo after construction (called from main).
func (h *ArticleHandler) SetTemplateRepo(templateRepo *repository.TemplateRepository, cfg *config.Config) {
	h.templateRepo = templateRepo
	h.cfg = cfg
}

// GetGrouped returns the /articles 分组 view: top-N category buckets plus
// an unclassified bucket. Filter semantics mirror GetAll. The response
// JSON keeps the "topic" key (now carrying a category enum slug, e.g.
// "ai_eng") for backward compatibility with the v1 frontend; the label
// map on the frontend renders it into the displayed Chinese name.
func (h *ArticleHandler) GetGrouped(c *gin.Context) {
	var feedID *int
	if fid := c.Query("feed_id"); fid != "" {
		if id, err := strconv.Atoi(fid); err == nil {
			feedID = &id
		}
	}
	unreadOnly := c.Query("unread") == "true"
	savedOnly := c.Query("saved") == "true"

	grouped, err := h.articleRepo.GetGroupedByCategory(getUserID(c), feedID, unreadOnly, savedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if grouped.Groups == nil {
		grouped.Groups = []model.TopicGroup{}
	}
	if grouped.Unclassified.Articles == nil {
		grouped.Unclassified.Articles = []model.Article{}
	}
	c.JSON(http.StatusOK, grouped)
}

func (h *ArticleHandler) GetAll(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	var feedID *int
	if fid := c.Query("feed_id"); fid != "" {
		id, err := strconv.Atoi(fid)
		if err == nil {
			feedID = &id
		}
	}

	unreadOnly := c.Query("unread") == "true"
	savedOnly := c.Query("saved") == "true"

	articles, err := h.articleRepo.GetAll(limit, offset, feedID, unreadOnly, savedOnly, getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) Search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit > 50 {
		limit = 50
	}
	articles, err := h.articleRepo.Search(query, getUserID(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) GetByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	article, feedType, err := h.articleRepo.GetByIDWithFeedType(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	userID := getUserID(c)
	progress, _ := h.progressRepo.GetByArticleAndUser(id, userID)
	signals, _ := h.prefRepo.GetUserSignals(userID, id)

	response := gin.H{
		"article":          article,
		"progress":         progress,
		"signals":          signals,
		"from_bookmarklet": feedType == "saved",
	}
	if article.LinksExtendable != nil && *article.LinksExtendable {
		children, err := h.articleRepo.GetChildren(article.ID)
		if err == nil {
			response["children"] = children
		} else {
			response["children"] = []model.Article{}
		}
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArticleHandler) GetRecommended(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	articles, err := h.articleRepo.GetRecommended(limit, getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) GenerateSummary(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	userID := getUserID(c)

	article, err := h.articleRepo.GetByID(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	// Determine which summarizer to use (user-custom or global)
	summarizerToUse := h.summarizer

	if h.templateRepo != nil && h.cfg != nil {
		aiCfg, err := h.templateRepo.GetUserAIConfig(userID)
		if err == nil && aiCfg != nil && aiCfg.APIKey != "" {
			// Build a temporary summarizer from the user's own key/url/model
			baseURL := aiCfg.BaseURL
			if baseURL == "" {
				baseURL = h.cfg.Claude.BaseURL
			}
			userSummarizer := ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
			summarizerToUse = service.NewSummarizerService(userSummarizer)
		}
	}

	// Parse optional template_id from JSON body or query
	var bodyReq struct {
		TemplateID int `json:"template_id"`
	}
	if h.templateRepo != nil {
		_ = c.ShouldBindJSON(&bodyReq)
		if templateIDStr := c.Query("template_id"); bodyReq.TemplateID == 0 && templateIDStr != "" {
			bodyReq.TemplateID, _ = strconv.Atoi(templateIDStr)
		}
	}

	if c.Query("stream") == "1" {
		h.streamSummary(c, id, article, summarizerToUse, bodyReq.TemplateID)
		return
	}

	var brief, detailed string

	if h.templateRepo != nil && bodyReq.TemplateID > 0 {
		tpl, terr := h.templateRepo.GetByID(bodyReq.TemplateID)
		if terr == nil && tpl != nil {
			brief, detailed, err = summarizerToUse.SummarizeWithTemplate(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"summary_brief":    brief,
				"summary_detailed": detailed,
			})
			return
		}
	}

	// Default summarization
	brief, detailed, err = summarizerToUse.Summarize(c.Request.Context(), article)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary_brief":    brief,
		"summary_detailed": detailed,
	})
}

func (h *ArticleHandler) streamSummary(c *gin.Context, id int, article *model.Article, summarizerToUse *service.SummarizerService, templateID int) {
	c.Writer.Header().Set("Content-Type", "application/x-ndjson")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeFrame(c, map[string]any{"type": "error", "msg": "streaming unsupported"})
		return
	}

	writeAndFlush := func(frame map[string]any) {
		writeFrame(c, frame)
		flusher.Flush()
	}

	briefDone := false
	onBrief := func(delta string) {
		writeAndFlush(map[string]any{"type": "brief_delta", "text": delta})
	}
	onDetailed := func(delta string) {
		// First detailed delta marks brief phase complete on the wire so the
		// client can switch its rendering pane even before we know the full text.
		if !briefDone {
			briefDone = true
			writeAndFlush(map[string]any{"type": "brief_phase_done"})
		}
		writeAndFlush(map[string]any{"type": "detailed_delta", "text": delta})
	}

	var brief, detailed string
	var serr error
	if h.templateRepo != nil && templateID > 0 {
		tpl, terr := h.templateRepo.GetByID(templateID)
		if terr == nil && tpl != nil {
			brief, detailed, serr = summarizerToUse.SummarizeWithTemplateStream(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt, onBrief, onDetailed,
			)
		} else {
			brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
		}
	} else {
		brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
	}

	if serr != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": serr.Error()})
		return
	}

	writeAndFlush(map[string]any{"type": "brief_done", "text": brief})
	writeAndFlush(map[string]any{"type": "detailed_done", "text": detailed})

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": err.Error()})
		return
	}

	writeAndFlush(map[string]any{"type": "done"})
}

func writeFrame(c *gin.Context, frame map[string]any) {
	bs, err := json.Marshal(frame)
	if err != nil {
		return
	}
	c.Writer.Write(bs)
	c.Writer.Write([]byte("\n"))
}

func (h *ArticleHandler) RecordClick(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Click will be handled by preference handler
	c.Status(http.StatusOK)
}

// ExpandChild transitions a stub link_set child to 'processing' so the worker
// picks it up on its next cycle. 4xx if the article is not a stub.
func (h *ArticleHandler) ExpandChild(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	// Authorise: the article must be visible to this user.
	if _, err := h.articleRepo.GetByID(id, getUserID(c)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}
	n, err := h.articleRepo.UpdateProcessingState(id, "stub", "processing")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "文章不是 stub 状态"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"article_id": id, "state": "processing"})
}

func (h *ArticleHandler) GetLinkSetRecommended(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days <= 0 || days > 30 {
		days = 7
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	articles, err := h.articleRepo.GetLinkSetRecommendations(getUserID(c), days, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}
	c.JSON(http.StatusOK, articles)
}

// CandidateView is one extractable link as shown in the batch_fetch modal.
type CandidateView struct {
	Title          string `json:"title"`
	URL            string `json:"url"`
	EditorNote     string `json:"editor_note,omitempty"`
	AlreadyFetched bool   `json:"already_fetched"`
}

// GetCandidates re-extracts the article's outbound link candidates by fetching
// raw HTML on demand. Marks any candidate whose URL already exists as a child
// of this article so the frontend can disable it in the modal.
func (h *ArticleHandler) GetCandidates(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	article, err := h.articleRepo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	doc, err := h.contentFetcher.FetchHTMLDocument(ctx, article.URL)
	if err != nil || doc == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法获取原页面"})
		return
	}
	rawHTML, _ := doc.Html()
	cands := rss.ExtractCandidates(rawHTML, article.URL)

	// Pre-load existing children URLs to mark already_fetched.
	children, _ := h.articleRepo.GetChildren(id)
	existing := make(map[string]struct{}, len(children))
	for _, ch := range children {
		existing[ch.URL] = struct{}{}
	}

	out := make([]CandidateView, 0, len(cands))
	for _, cd := range cands {
		_, dup := existing[cd.URL]
		out = append(out, CandidateView{
			Title:          cd.Title,
			URL:            cd.URL,
			EditorNote:     cd.EditorNote,
			AlreadyFetched: dup,
		})
	}
	c.JSON(http.StatusOK, gin.H{"candidates": out})
}

// BatchFetchRequest is what the modal posts on confirm.
type BatchFetchRequest struct {
	Candidates []struct {
		Title      string `json:"title"`
		URL        string `json:"url"`
		EditorNote string `json:"editor_note"`
	} `json:"candidates"`
}

// BatchFetch creates child article rows for the user-selected candidates
// and queues them for content fetching (processing_state='processing').
func (h *ArticleHandler) BatchFetch(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	parent, err := h.articleRepo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}
	var req BatchFetchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Candidates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no candidates selected"})
		return
	}
	inputs := make([]repository.LinkSetChildInput, 0, len(req.Candidates))
	for _, cand := range req.Candidates {
		if cand.URL == "" {
			continue
		}
		title := cand.Title
		if title == "" {
			title = cand.URL
		}
		inputs = append(inputs, repository.LinkSetChildInput{
			FeedID:          parent.FeedID,
			ParentArticleID: parent.ID,
			Title:           title,
			URL:             cand.URL,
			EditorNote:      cand.EditorNote,
			PrerankScore:    0,
			ProcessingState: "processing",
			PublishedAt:     parent.PublishedAt,
		})
	}
	n, err := h.articleRepo.InsertLinkSetChildren(inputs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"inserted": n})
}
