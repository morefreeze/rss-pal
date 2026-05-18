package api

import (
	"database/sql"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/gin-gonic/gin"
)

// maxUploadBytes caps a restore-upload request body. Bigger than any plausible
// real backup but small enough that an accidental gigantic upload doesn't
// fill /tmp on the admin host.
const maxUploadBytes = 200 << 20 // 200 MiB

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

// RestoreBackupUpload restores from a user-supplied backup pair uploaded as
// multipart/form-data. The on-disk backup dir is not touched — uploaded files
// land in a private temp dir that is removed before returning.
//
// Form fields:
//
//	metadata  (required) — the .json snapshot file
//	saved     (optional) — the .saved.json.gz sibling. If absent, restore
//	                        falls back to metadata-only (legacy-backup) mode.
//
// Files are renamed to a fixed pair on disk (restore-upload.json +
// restore-upload.saved.json.gz) so the original (untrusted) filename never
// reaches the filesystem and the sibling pairing is unambiguous.
func (h *AdminHandler) RestoreBackupUpload(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)

	metaHeader, err := c.FormFile("metadata")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metadata file required: " + err.Error()})
		return
	}
	if !strings.HasSuffix(strings.ToLower(metaHeader.Filename), ".json") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metadata file must end in .json"})
		return
	}

	var savedHeader *multipart.FileHeader
	if form, _ := c.MultipartForm(); form != nil {
		if hs := form.File["saved"]; len(hs) > 0 {
			savedHeader = hs[0]
			lower := strings.ToLower(savedHeader.Filename)
			if !strings.HasSuffix(lower, ".saved.json.gz") && !strings.HasSuffix(lower, ".json.gz") {
				c.JSON(http.StatusBadRequest, gin.H{"error": "saved file must end in .saved.json.gz"})
				return
			}
		}
	}

	tmpDir, err := os.MkdirTemp("", "rss-pal-restore-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mkdir tmp: " + err.Error()})
		return
	}
	defer os.RemoveAll(tmpDir)

	metaPath := filepath.Join(tmpDir, "restore-upload.json")
	if err := saveUpload(metaHeader, metaPath); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "save metadata: " + err.Error()})
		return
	}

	if savedHeader != nil {
		// LoadSaved derives the sibling path by replacing .json with
		// .saved.json.gz, so writing it at this fixed name pairs automatically.
		savedPath := strings.TrimSuffix(metaPath, ".json") + ".saved.json.gz"
		if err := saveUpload(savedHeader, savedPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "save saved sibling: " + err.Error()})
			return
		}
	}

	s, err := backup.Load(metaPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse metadata: " + err.Error()})
		return
	}
	ss, err := backup.LoadSaved(metaPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse saved sibling: " + err.Error()})
		return
	}
	stats, err := backup.Restore(c.Request.Context(), h.db, s, ss)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "stats": stats})
}

func saveUpload(fh *multipart.FileHeader, dst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
