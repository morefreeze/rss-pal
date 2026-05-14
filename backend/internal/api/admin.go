package api

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/gin-gonic/gin"
)

// AdminHandler exposes the backup/restore endpoints. Authentication is
// enforced by the route group's middleware; admin-only checks happen here so
// non-admins can't enumerate or restore backups even if they have a valid
// JWT.
type AdminHandler struct {
	db     *sql.DB
	runner *backup.Runner
	cfg    *config.Config
}

func NewAdminHandler(db *sql.DB, runner *backup.Runner, cfg *config.Config) *AdminHandler {
	return &AdminHandler{db: db, runner: runner, cfg: cfg}
}

func (h *AdminHandler) requireAdmin(c *gin.Context) bool {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return false
	}
	return true
}

// ListBackups returns all backup files on disk, newest first. Each entry has
// name + UTC timestamp + size in bytes.
func (h *AdminHandler) ListBackups(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	files, err := backup.List(h.cfg.Backup.Dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": files, "dir": h.cfg.Backup.Dir})
}

// CreateBackupNow forces a snapshot immediately, bypassing the debounce. Used
// by the "back up now" button in the UI.
func (h *AdminHandler) CreateBackupNow(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	if h.runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup not configured"})
		return
	}
	if err := h.runner.RunNow(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RestoreBackup loads a named backup from disk and upserts it into the DB.
// Path traversal is guarded by enforcing that the filename has no slash and
// resolves under the configured backup dir.
func (h *AdminHandler) RestoreBackup(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if strings.ContainsAny(req.Name, `/\`) || strings.Contains(req.Name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
		return
	}
	path := filepath.Join(h.cfg.Backup.Dir, req.Name)
	s, err := backup.Load(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	// Sibling may be absent (legacy backup pre-saved-snapshot) — LoadSaved
	// returns (nil, nil) in that case and Restore handles it.
	ss, err := backup.LoadSaved(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load saved sibling: " + err.Error()})
		return
	}
	stats, err := backup.Restore(c.Request.Context(), h.db, s, ss)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "stats": stats})
}
