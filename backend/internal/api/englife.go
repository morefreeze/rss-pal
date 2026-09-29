package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/bytedance/rss-pal/internal/englife"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type EnglifeHandler struct {
	db      *sql.DB
	service *englife.Service
}

func NewEnglifeHandler(db *sql.DB, s *englife.Service) *EnglifeHandler { return &EnglifeHandler{db, s} }
func (h *EnglifeHandler) admin(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	var admin bool
	err := h.db.QueryRowContext(c.Request.Context(), `SELECT is_admin FROM users WHERE id=$1`, getUserID(c)).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !admin {
		c.JSON(403, gin.H{"error": "仅管理员可以管理第三方服务"})
		return false
	}
	if err != nil {
		c.JSON(503, gin.H{"error": "账号权限暂不可用"})
		return false
	}
	return true
}
func englifeError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	message := "englife 暂不可用，请稍后检查连接"
	switch {
	case errors.Is(err, englife.ErrPair):
		code = 400
		message = "授权已过期或已使用，请重新发起连接"
	case errors.Is(err, englife.ErrExpired):
		code = 409
		message = "englife 登录已失效，请重新登录并授权"
	case errors.Is(err, englife.ErrQuota):
		code = 409
		message = "englife 额度不足，已暂停处理"
	case errors.Is(err, englife.ErrPending):
		code = 409
		message = "连接正在使用，请稍后重试"
	case errors.Is(err, englife.ErrDisabled):
		code = 409
		message = "服务尚未配置会话加密密钥"
	}
	c.JSON(code, gin.H{"error": message})
}
func (h *EnglifeHandler) Get(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	st, err := h.service.Status(c.Request.Context())
	if err != nil {
		englifeError(c, err)
		return
	}
	c.JSON(200, st)
}
func (h *EnglifeHandler) Pair(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	p, err := h.service.Pair(c.Request.Context(), getUserID(c))
	if err != nil {
		englifeError(c, err)
		return
	}
	c.JSON(200, p)
}
func (h *EnglifeHandler) Check(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	st, err := h.service.Check(ctx)
	if err != nil {
		englifeError(c, err)
		return
	}
	c.JSON(200, st)
}
func (h *EnglifeHandler) Disconnect(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	if err := h.service.Disconnect(c.Request.Context()); err != nil {
		englifeError(c, err)
		return
	}
	st, err := h.service.Status(c.Request.Context())
	if err != nil {
		englifeError(c, err)
		return
	}
	c.JSON(200, st)
}

// Complete is authorized only by a high-entropy, single-use, administrator-bound
// token. No JWT or third-party credentials are reflected back to the browser.
func (h *EnglifeHandler) Complete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 48*1024)
	var req struct {
		Token   string           `json:"token"`
		Cookies []englife.Cookie `json:"cookies"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Token) != 43 || len(req.Cookies) > 80 {
		c.JSON(400, gin.H{"error": "授权数据无效，请重新连接"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	if err := h.service.Complete(ctx, req.Token, req.Cookies); err != nil {
		englifeError(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
