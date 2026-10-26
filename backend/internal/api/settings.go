package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	templateRepo *repository.TemplateRepository
	userRepo     *repository.UserRepository
	cfg          *config.Config
	summarizer   *ai.Summarizer
}

func NewSettingsHandler(cfg *config.Config, templateRepo *repository.TemplateRepository, userRepo *repository.UserRepository) *SettingsHandler {
	var summarizer *ai.Summarizer
	if cfg.Claude.APIKey != "" {
		summarizer = ai.NewSummarizerWithModel(cfg.Claude.APIKey, cfg.Claude.BaseURL, cfg.Claude.Model)
	}
	return &SettingsHandler{cfg: cfg, templateRepo: templateRepo, userRepo: userRepo, summarizer: summarizer}
}

// GetTemplates GET /api/templates — 返回系统模板 + 用户自己的模板列表
func (h *SettingsHandler) GetTemplates(c *gin.Context) {
	userID := getUserID(c)

	templateRepo := h.templateRepo.WithCtx(c)
	systemTemplates, err := templateRepo.GetSystemTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	userTemplates, err := templateRepo.GetUserTemplates(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	all := make([]model.SummaryTemplate, 0, len(systemTemplates)+len(userTemplates))
	all = append(all, systemTemplates...)
	all = append(all, userTemplates...)

	c.JSON(http.StatusOK, all)
}

// CreateTemplate POST /api/templates — 创建用户模板
func (h *SettingsHandler) CreateTemplate(c *gin.Context) {
	userID := getUserID(c)

	var req struct {
		Name           string `json:"name" binding:"required"`
		Description    string `json:"description"`
		Style          string `json:"style"`
		BriefPrompt    string `json:"brief_prompt" binding:"required"`
		DetailedPrompt string `json:"detailed_prompt" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	t := &model.SummaryTemplate{
		UserID:         &userID,
		Name:           req.Name,
		Description:    req.Description,
		Style:          req.Style,
		BriefPrompt:    req.BriefPrompt,
		DetailedPrompt: req.DetailedPrompt,
		IsSystem:       false,
	}
	if err := h.templateRepo.WithCtx(c).CreateUserTemplate(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, t)
}

// DeleteTemplate DELETE /api/templates/:id — 删除用户自己的模板
func (h *SettingsHandler) DeleteTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	userID := getUserID(c)
	if err := h.templateRepo.WithCtx(c).DeleteUserTemplate(id, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// SetDefaultTemplate PUT /api/settings/template — 设置用户默认模板
func (h *SettingsHandler) SetDefaultTemplate(c *gin.Context) {
	var req struct {
		TemplateID int `json:"template_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := getUserID(c)
	if err := h.templateRepo.WithCtx(c).SetUserDefaultTemplate(userID, req.TemplateID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "default template updated"})
}

// Legacy endpoints deliberately never read, parse, echo or store credentials.
func (h *SettingsHandler) GetAIConfig(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"code": "local_ai_only", "error": "API Key 已改为仅保存在本机浏览器，请在设置中重新配置；需要手动点击生成"})
}

func (h *SettingsHandler) SaveAIConfig(c *gin.Context) {
	h.GetAIConfig(c)
}

// PolishPrompt POST /api/settings/polish-prompt — AI 润色摘要 prompt
func (h *SettingsHandler) PolishPrompt(c *gin.Context) {
	if h.summarizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required,max=8000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	polished, err := h.summarizer.Polish(ctx, req.Content)
	if err != nil {
		if writeTaskBudgetError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI 润色失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"polished": polished})
}

// GetBookmarkletToken GET /api/settings/bookmarklet-token — 返回当前用户的
// bookmarklet token；未生成过返回 token=null。
func (h *SettingsHandler) GetBookmarkletToken(c *gin.Context) {
	userID := getUserID(c)
	token, err := h.userRepo.WithCtx(c).GetBookmarkletToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"token": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// RegenerateBookmarkletToken POST /api/settings/bookmarklet-token/regenerate
// — 生成新的 token，旧 token 立即失效。
func (h *SettingsHandler) RegenerateBookmarkletToken(c *gin.Context) {
	userID := getUserID(c)
	token, err := GenerateBookmarkletToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 token 失败"})
		return
	}
	if err := h.userRepo.WithCtx(c).SetBookmarkletToken(userID, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 token 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}
