package api

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) SetNickname(c *gin.Context) {
	var req struct {
		Nickname  string `json:"nickname" binding:"required,max=128"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirmed {
		c.JSON(400, gin.H{"error": "请填写昵称，并确认设置后无法修改"})
		return
	}
	name := strings.TrimSpace(req.Nickname)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 32 || strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		c.JSON(400, gin.H{"error": "昵称须为 1–32 个字符，不能包含控制字符"})
		return
	}
	repo := h.userRepo.WithCtx(c)
	err := repo.SetNickname(getUserID(c), name)
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
