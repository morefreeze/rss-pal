package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/bytedance/rss-pal/internal/airouting"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"time"
)

type AdminAIHandler struct {
	db     *sql.DB
	store  *airouting.Store
	client *http.Client
}

func NewAdminAIHandler(db *sql.DB, store *airouting.Store) *AdminAIHandler {
	return &AdminAIHandler{db, store, &http.Client{Timeout: 20 * time.Second}}
}
func (h *AdminAIHandler) RequireAdmin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if getUserID(c) <= 0 {
		c.AbortWithStatusJSON(401, gin.H{"error": "请先登录"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var admin bool
	err := h.db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id=$1`, getUserID(c)).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !admin {
		c.AbortWithStatusJSON(403, gin.H{"error": "仅管理员可配置平台 AI"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "暂时无法确认管理员权限"})
		return
	}
	c.Next()
}
func aiConfigError(c *gin.Context, err error) {
	status, msg := 503, "AI 配置暂时不可用，请稍后重试"
	var invalid *airouting.InvalidError
	if errors.As(err, &invalid) {
		status, msg = 400, invalid.Error()
	}
	if errors.Is(err, airouting.ErrConflict) {
		status, msg = 409, err.Error()
	}
	c.JSON(status, gin.H{"error": msg})
}
func decodeAIConfig(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		c.JSON(400, gin.H{"error": "配置格式无效"})
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		c.JSON(400, gin.H{"error": "配置格式无效"})
		return false
	}
	return true
}
func (h *AdminAIHandler) Get(c *gin.Context) {
	v, e := h.store.View(c.Request.Context())
	if e != nil {
		aiConfigError(c, e)
		return
	}
	c.JSON(200, v)
}
func (h *AdminAIHandler) Save(c *gin.Context) {
	var u airouting.Update
	if !decodeAIConfig(c, &u) {
		return
	}
	v, e := h.store.Save(c.Request.Context(), u)
	if e != nil {
		aiConfigError(c, e)
		return
	}
	c.JSON(200, v)
}
func (h *AdminAIHandler) Models(c *gin.Context) {
	var input struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"api_key"`
	}
	if !decodeAIConfig(c, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	key, e := h.store.Key(ctx, input.Provider, input.Endpoint, input.APIKey)
	if e != nil {
		aiConfigError(c, e)
		return
	}
	models, e := airouting.FetchModels(ctx, h.client, input.Provider, input.Endpoint, key)
	if e != nil {
		c.JSON(502, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, models)
}
