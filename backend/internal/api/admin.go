package api

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"fmt"
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

// DownloadBackup serves one backup, picked by metadata-file name. If the
// saved-archive sibling exists, both files are streamed as a single .tar.gz
// so the user always gets exactly one file per click. If there is no
// sibling, the plain .json is sent unchanged.
//
// Path traversal is blocked the same way as Restore — name must have no
// slashes and no `..`. The filename must end in `.json` (the metadata
// pointer); we resolve the sibling internally.
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
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be a backup .json"})
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
		// No sibling — single file, serve as-is.
		c.FileAttachment(metaPath, name)
		return
	}

	// Pair — bundle as tar.gz so the user gets one file per click and the
	// inner structure stays restorable in one shot.
	archiveName := strings.TrimSuffix(name, ".json") + ".tar.gz"
	c.Header("Content-Disposition", `attachment; filename="`+archiveName+`"`)
	c.Header("Content-Type", "application/gzip")

	gz := gzip.NewWriter(c.Writer)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := writeTarFile(tw, metaPath, name, metaInfo); err != nil {
		// Headers already flushed — best-effort log via response is impossible.
		// Truncating the stream is the only signal the client gets.
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

// extractBackupArchive walks the tar.gz, picking out the .json member
// (required) and the .saved.json.gz member (optional). Any other member is
// skipped; absolute paths and `..` entries are rejected (tar-slip).
func extractBackupArchive(fh *multipart.FileHeader, metaDst, savedDst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var foundMeta bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == "" || base == "." || strings.Contains(hdr.Name, "..") || strings.HasPrefix(hdr.Name, "/") {
			return fmt.Errorf("rejected tar entry: %q", hdr.Name)
		}
		lower := strings.ToLower(base)
		switch {
		case strings.HasSuffix(lower, ".saved.json.gz"):
			if err := writeStream(tr, savedDst); err != nil {
				return fmt.Errorf("write saved member: %w", err)
			}
		case strings.HasSuffix(lower, ".json"):
			if err := writeStream(tr, metaDst); err != nil {
				return fmt.Errorf("write metadata member: %w", err)
			}
			foundMeta = true
		}
	}
	if !foundMeta {
		return fmt.Errorf("archive contains no .json metadata member")
	}
	return nil
}

func writeStream(src io.Reader, dst string) error {
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

func saveUpload(fh *multipart.FileHeader, dst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	return writeStream(src, dst)
}
