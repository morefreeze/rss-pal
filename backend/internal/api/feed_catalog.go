package api

import (
	"database/sql"
	"errors"
	service "github.com/bytedance/rss-pal/internal/feedcatalog"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type FeedCatalogHandler struct {
	db      *sql.DB
	service *service.FeedCatalogService
}

func NewFeedCatalogHandler(db *sql.DB, s *service.FeedCatalogService) *FeedCatalogHandler {
	return &FeedCatalogHandler{db, s}
}
func (h *FeedCatalogHandler) RequireAdmin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	uid := getUserID(c)
	if uid <= 0 {
		c.AbortWithStatusJSON(401, gin.H{"error": "请先登录"})
		return
	}
	var admin bool
	err := h.db.QueryRowContext(c.Request.Context(), `SELECT is_admin FROM users WHERE id=$1`, uid).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !admin {
		c.AbortWithStatusJSON(403, gin.H{"error": "仅管理员可管理公共推荐目录"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "暂时无法确认管理员权限"})
		return
	}
	c.Next()
}
func catalogError(c *gin.Context, err error) {
	code, msg := 500, "目录操作失败，请重试"
	switch {
	case errors.Is(err, repository.ErrCatalogConflict):
		code = 409
		msg = err.Error()
	case errors.Is(err, service.ErrCatalogInput):
		code = 400
		msg = err.Error()
	case errors.Is(err, service.ErrCatalogCheck):
		code = 422
		msg = err.Error()
	case errors.Is(err, sql.ErrNoRows):
		code = 404
		msg = "目录条目不存在"
	}
	c.JSON(code, gin.H{"error": msg})
}
func (h *FeedCatalogHandler) PublicList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if getUserID(c) <= 0 {
		c.JSON(401, gin.H{"error": "请先登录"})
		return
	}
	entries, err := h.service.Repo.List(c.Request.Context(), true)
	if err != nil {
		catalogError(c, err)
		return
	}
	out := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		out = append(out, gin.H{"id": e.ID, "title": e.Title, "url": e.URL, "category": e.Category, "description": e.Description, "sort_order": e.SortOrder})
	}
	c.JSON(200, out)
}
func (h *FeedCatalogHandler) List(c *gin.Context) {
	entries, err := h.service.Repo.List(c.Request.Context(), false)
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(200, entries)
}
func catalogID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "无效目录编号"})
		return 0, false
	}
	return id, true
}
func (h *FeedCatalogHandler) save(c *gin.Context, id int) {
	var in repository.FeedCatalogInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(&in); err != nil {
		catalogError(c, service.ErrCatalogInput)
		return
	}
	e, err := h.service.Save(c.Request.Context(), id, getUserID(c), in)
	if err != nil {
		catalogError(c, err)
		return
	}
	code := 200
	if id == 0 {
		code = 201
	}
	c.JSON(code, e)
}
func (h *FeedCatalogHandler) Create(c *gin.Context) { h.save(c, 0) }
func (h *FeedCatalogHandler) Update(c *gin.Context) {
	if id, ok := catalogID(c); ok {
		h.save(c, id)
	}
}
func (h *FeedCatalogHandler) Check(c *gin.Context)       { h.check(c, false) }
func (h *FeedCatalogHandler) Publication(c *gin.Context) { h.check(c, true) }
func (h *FeedCatalogHandler) check(c *gin.Context, publication bool) {
	id, ok := catalogID(c)
	if !ok {
		return
	}
	var in struct {
		Published *bool `json:"published"`
		Revision  int   `json:"revision"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&in); err != nil || in.Revision < 1 || publication && in.Published == nil {
		c.JSON(400, gin.H{"error": "请提供发布状态和当前版本"})
		return
	}
	var e repository.FeedCatalogEntry
	var err error
	if publication && !*in.Published {
		e, err = h.service.Repo.Unpublish(c.Request.Context(), id, getUserID(c), in.Revision)
	} else {
		e, err = h.service.Check(c.Request.Context(), id, getUserID(c), in.Revision, publication)
	}
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(200, e)
}
