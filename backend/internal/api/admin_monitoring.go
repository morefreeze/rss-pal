package api

import (
	"context"
	"database/sql"
	"github.com/bytedance/rss-pal/internal/opsmonitor"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

type AdminMonitoringHandler struct {
	db      *sql.DB
	service *opsmonitor.Service
}

func NewAdminMonitoringHandler(db *sql.DB, s *opsmonitor.Service) *AdminMonitoringHandler {
	return &AdminMonitoringHandler{db, s}
}
func (h *AdminMonitoringHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var admin bool
	err := h.db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id=$1`, getUserID(c)).Scan(&admin)
	if err == sql.ErrNoRows || err == nil && !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	if err != nil {
		c.JSON(503, gin.H{"error": "运行监控暂时不可用"})
		return
	}
	hours, err := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if err != nil || (hours != 1 && hours != 24 && hours != 168) {
		c.JSON(400, gin.H{"error": "hours must be 1, 24 or 168"})
		return
	}
	before, err := strconv.ParseInt(c.DefaultQuery("before_id", "0"), 10, 64)
	if err != nil || before < 0 {
		c.JSON(400, gin.H{"error": "invalid before_id"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 50 {
		c.JSON(400, gin.H{"error": "limit must be 1..50"})
		return
	}
	r, err := h.service.Snapshot(c.Request.Context(), hours, before, limit)
	if err != nil {
		c.JSON(503, gin.H{"error": "运行监控暂时不可用"})
		return
	}
	c.JSON(200, r)
}
