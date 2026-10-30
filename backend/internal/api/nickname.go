package api

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/nickname"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) MarkNicknamePrompted(c *gin.Context) {
	err := h.userRepo.WithCtx(c).MarkNicknamePrompted(getUserID(c))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "提示状态保存失败，请稍后重试"})
		return
	}
	jsonAfterCommit(c, 200, gin.H{"ok": true})
}

func (h *AuthHandler) SetNickname(c *gin.Context) {
	var req struct {
		Nickname  string `json:"nickname" binding:"required,max=128"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirmed {
		c.JSON(400, gin.H{"error": "请填写昵称，并确认设置后无法修改"})
		return
	}
	repo := h.userRepo.WithCtx(c)
	err := repo.SetNickname(getUserID(c), req.Nickname)
	if errors.Is(err, nickname.ErrFormat) || errors.Is(err, nickname.ErrRestricted) {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, repository.ErrNicknameLocked) {
		c.JSON(409, gin.H{"error": "昵称已设置，无法再次修改"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "昵称保存失败，请稍后重试"})
		return
	}
	user, err := repo.FindByID(getUserID(c))
	if err != nil || user == nil {
		c.JSON(500, gin.H{"error": "账号信息读取失败，请刷新页面"})
		return
	}
	jsonAfterCommit(c, 200, user)
}
