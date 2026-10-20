package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type ShortcutHandler struct {
	repo *repository.ShortcutRepository
}

func NewShortcutHandler(r *repository.ShortcutRepository) *ShortcutHandler {
	return &ShortcutHandler{repo: r}
}
func (h *ShortcutHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	n, err := h.repo.WithCtx(c).Count(getUserID(c))
	if err != nil {
		c.JSON(500, gin.H{"error": "无法读取快捷指令连接"})
		return
	}
	c.JSON(200, gin.H{"connections": n})
}
func (h *ShortcutHandler) Pair(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	code, expires, err := h.repo.WithCtx(c).Pair(getUserID(c))
	if err != nil {
		c.JSON(500, gin.H{"error": "无法创建连接，请重试"})
		return
	}
	jsonAfterCommit(c, 200, gin.H{"code": code, "expires_at": expires})
}
func (h *ShortcutHandler) Connect(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "连接请求无效，请回到 RSS Pal 设置重新连接"})
		return
	}
	token, err := h.repo.Connect(req.Code)
	if errors.Is(err, repository.ErrShortcutConnectionInvalid) {
		c.JSON(401, gin.H{"error": "连接已过期或使用过，请回到 RSS Pal 设置重新连接"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "连接失败，请重试"})
		return
	}
	c.JSON(200, gin.H{"token": token})
}
func (h *ShortcutHandler) Revoke(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := h.repo.WithCtx(c).Revoke(getUserID(c)); err != nil {
		c.JSON(500, gin.H{"error": "无法断开连接，请重试"})
		return
	}
	jsonAfterCommit(c, 200, gin.H{"connections": 0})
}
func (h *ShortcutHandler) ResolveOwner(c *gin.Context, tx *sql.Tx) (int, error) {
	c.Header("Cache-Control", "no-store")
	uid, err := repository.ResolveShortcutOwner(tx, bearerToken(c))
	if errors.Is(err, repository.ErrShortcutConnectionInvalid) {
		return 0, ErrPublicTokenInvalid
	}
	return uid, err
}
