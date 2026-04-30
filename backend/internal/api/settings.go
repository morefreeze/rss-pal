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
	cfg          *config.Config
	summarizer   *ai.Summarizer
}

func NewSettingsHandler(cfg *config.Config, templateRepo *repository.TemplateRepository) *SettingsHandler {
	var summarizer *ai.Summarizer
	if cfg.Claude.APIKey != "" {
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
	}
	return &SettingsHandler{cfg: cfg, templateRepo: templateRepo, summarizer: summarizer}
}

// GetTemplates GET /api/templates — 返回系统模板 + 用户自己的模板列表
func (h *SettingsHandler) GetTemplates(c *gin.Context) {
	userID := getUserID(c)

	systemTemplates, err := h.templateRepo.GetSystemTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	userTemplates, err := h.templateRepo.GetUserTemplates(userID)
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
	if err := h.templateRepo.CreateUserTemplate(t); err != nil {
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
	if err := h.templateRepo.DeleteUserTemplate(id, userID); err != nil {
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
	if err := h.templateRepo.SetUserDefaultTemplate(userID, req.TemplateID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "default template updated"})
}

// GetAIConfig GET /api/settings/ai — 获取用户 AI 配置（api_key 脱敏）
func (h *SettingsHandler) GetAIConfig(c *gin.Context) {
	userID := getUserID(c)

	cfg, err := h.templateRepo.GetUserAIConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	// 脱敏 api_key：只返回前4位 + ***
	masked := cfg.APIKey
	if len(masked) > 4 {
		masked = masked[:4] + "***"
	}
	cfg.APIKey = masked

	c.JSON(http.StatusOK, cfg)
}

// SaveAIConfig PUT /api/settings/ai — 保存用户 AI 配置
func (h *SettingsHandler) SaveAIConfig(c *gin.Context) {
	var req struct {
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := getUserID(c)

	// If api_key is empty, preserve existing key
	if req.APIKey == "" {
		existing, _ := h.templateRepo.GetUserAIConfig(userID)
		if existing != nil {
			req.APIKey = existing.APIKey
		}
	}

	cfg := &model.UserAIConfig{
		UserID:  userID,
		APIKey:  req.APIKey,
		BaseURL: req.BaseURL,
		Model:   req.Model,
	}
	if err := h.templateRepo.UpsertUserAIConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 返回时脱敏
	if len(cfg.APIKey) > 4 {
		cfg.APIKey = cfg.APIKey[:4] + "***"
	}

	c.JSON(http.StatusOK, cfg)
}

// PolishPrompt POST /api/settings/polish-prompt — AI 润色摘要 prompt
func (h *SettingsHandler) PolishPrompt(c *gin.Context) {
	if h.summarizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	polished, err := h.summarizer.Polish(ctx, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI 润色失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"polished": polished})
}
