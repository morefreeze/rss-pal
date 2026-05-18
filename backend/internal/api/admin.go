package api

import (
	"archive/tar"
	"compress/gzip"
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
// Accepts either the canonical .tar.gz format (extracted to a temp dir) or
// the legacy .json (which uses the sibling-lookup path). A .tar.gz whose
// contents cannot be parsed surfaces a 400 — there is no silent fallback,
// per spec.
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
	lower := strings.ToLower(req.Name)
	if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".json") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must end in .tar.gz or .json"})
		return
	}
	path := filepath.Join(h.cfg.Backup.Dir, req.Name)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	var (
		metaPath  = path
		savedPath = ""
		cleanup   = func() {}
	)
	if strings.HasSuffix(lower, ".tar.gz") {
		tmpDir, err := os.MkdirTemp("", "rss-pal-restore-disk-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "mkdir tmp: " + err.Error()})
			return
		}
		cleanup = func() { os.RemoveAll(tmpDir) }
		defer cleanup()
		metaPath = filepath.Join(tmpDir, "restore.json")
		savedPath = filepath.Join(tmpDir, "restore.saved.json.gz")
		if _, err := backup.ExtractTarball(path, metaPath, savedPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "extract tarball: " + err.Error()})
			return
		}
	}

	s, err := backup.Load(metaPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse metadata: " + err.Error()})
		return
	}
	// LoadSaved derives the sibling path from metaPath. For legacy .json this
	// finds the .saved.json.gz next to it; for the tarball-extracted case the
	// extracted savedPath is already named correctly.
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

// DownloadBackup serves a backup as exactly one file:
//   - .tar.gz on disk          → served as-is
//   - .json on disk, no sibling → served as-is (legacy)
//   - .json on disk, has sibling (legacy pair) → bundled on the fly as .tar.gz
//
// Path traversal is blocked the same way as Restore — name must have no
// slashes and no `..`.
func (h *AdminHandler) DownloadBackup(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
		return
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		path := filepath.Join(h.cfg.Backup.Dir, name)
		if _, err := os.Stat(path); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.FileAttachment(path, name)
		return
	case strings.HasSuffix(lower, ".json"):
		// fallthrough to legacy bundling below
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must end in .tar.gz or .json"})
		return
	}

	metaPath := filepath.Join(h.cfg.Backup.Dir, name)
	metaInfo, err := os.Stat(metaPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	savedName := strings.TrimSuffix(name, ".json") + ".saved.json.gz"
	savedPath := filepath.Join(h.cfg.Backup.Dir, savedName)
	savedInfo, savedErr := os.Stat(savedPath)
	if savedErr != nil {
		// Solo legacy .json — single file, serve as-is.
		c.FileAttachment(metaPath, name)
		return
	}

	// Legacy pair — bundle on the fly so the user gets one file per click.
	archiveName := strings.TrimSuffix(name, ".json") + ".tar.gz"
	c.Header("Content-Disposition", `attachment; filename="`+archiveName+`"`)
	c.Header("Content-Type", "application/gzip")

	gz := gzip.NewWriter(c.Writer)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := writeTarFile(tw, metaPath, name, metaInfo); err != nil {
		return
	}
	_ = writeTarFile(tw, savedPath, savedName, savedInfo)
}

func writeTarFile(tw *tar.Writer, path, name string, info os.FileInfo) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

// RestoreBackupUpload restores from a user-supplied backup uploaded as
// multipart/form-data. The on-disk backup dir is not touched — uploaded
// files land in a private temp dir that is removed before returning.
//
// Two accepted shapes:
//
//	archive   — a single .tar.gz / .tgz containing the .json (and optional
//	            .saved.json.gz). This is what DownloadBackup emits for paired
//	            backups, so download-then-upload is a one-file round trip.
//	metadata  — the .json snapshot file, with optional `saved` field for the
//	            .saved.json.gz sibling. Used when the user has the raw pair.
//
// If `archive` is present, it wins and the other fields are ignored.
// On-disk filenames inside the tmp dir are normalised to restore-upload.json
// + restore-upload.saved.json.gz so the original (untrusted) names never
// touch the filesystem.
func (h *AdminHandler) RestoreBackupUpload(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)

	tmpDir, err := os.MkdirTemp("", "rss-pal-restore-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mkdir tmp: " + err.Error()})
		return
	}
	defer os.RemoveAll(tmpDir)

	metaPath := filepath.Join(tmpDir, "restore-upload.json")
	savedPath := strings.TrimSuffix(metaPath, ".json") + ".saved.json.gz"

	form, _ := c.MultipartForm()

	var archiveHeader *multipart.FileHeader
	if form != nil {
		if hs := form.File["archive"]; len(hs) > 0 {
			archiveHeader = hs[0]
		}
	}

	if archiveHeader != nil {
		lower := strings.ToLower(archiveHeader.Filename)
		if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "archive must be .tar.gz or .tgz"})
			return
		}
		if err := extractBackupArchive(archiveHeader, metaPath, savedPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "extract archive: " + err.Error()})
			return
		}
	} else {
		metaHeader, err := c.FormFile("metadata")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "metadata or archive file required"})
			return
		}
		if !strings.HasSuffix(strings.ToLower(metaHeader.Filename), ".json") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "metadata file must end in .json"})
			return
		}
		if err := saveUpload(metaHeader, metaPath); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "save metadata: " + err.Error()})
			return
		}
		if form != nil {
			if hs := form.File["saved"]; len(hs) > 0 {
				savedHeader := hs[0]
				lower := strings.ToLower(savedHeader.Filename)
				if !strings.HasSuffix(lower, ".saved.json.gz") && !strings.HasSuffix(lower, ".json.gz") {
					c.JSON(http.StatusBadRequest, gin.H{"error": "saved file must end in .saved.json.gz"})
					return
				}
				if err := saveUpload(savedHeader, savedPath); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "save saved sibling: " + err.Error()})
					return
				}
			}
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

func extractBackupArchive(fh *multipart.FileHeader, metaDst, savedDst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = backup.ExtractTarballStream(src, metaDst, savedDst)
	return err
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
