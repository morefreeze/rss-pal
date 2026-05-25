package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/pdfextract"
	"github.com/gin-gonic/gin"
)

// AccessCheck verifies that the caller in c is allowed to read articleID.
// Returns (true, nil) when allowed, (false, nil) when denied, or
// (false, err) on infrastructure errors. Implemented as a func type so
// tests can inject simple stubs without spinning up the full auth stack.
type AccessCheck func(c *gin.Context, articleID int) (allowed bool, err error)

// ArticleImageHandler serves PDF-extracted image assets stored under
// baseDir/article_images/<articleID>/<idx>.<ext>. Authorization is
// delegated to an AccessCheck so the handler stays decoupled from the
// repository layer and is trivially testable.
type ArticleImageHandler struct {
	baseDir string
	access  AccessCheck
}

// NewArticleImageHandler wires the handler with the image base directory
// (typically cfg.Backup.Dir, matching what pdfextract.WriteImages uses
// when persisting images) and an access-check function.
func NewArticleImageHandler(baseDir string, access AccessCheck) *ArticleImageHandler {
	return &ArticleImageHandler{baseDir: baseDir, access: access}
}

// Serve responds to GET /api/articles/:id/images/:idx where :idx is
// e.g. "3.png" or "0.jpg". Emits the file with a long-lived immutable
// Cache-Control plus an ETag (first 16 hex chars of SHA-256), and honors
// If-None-Match with 304.
//
// The on-disk layout is resolved via pdfextract.ImagePath so writers
// (pdfextract.WriteImages) and readers (this handler) can never drift.
func (h *ArticleImageHandler) Serve(c *gin.Context) {
	idStr := c.Param("id")
	idxStr := c.Param("idx")
	articleID, err := strconv.Atoi(idStr)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	allowed, err := h.access(c, articleID)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if !allowed {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// idxStr looks like "3.png" — split into numeric index + extension.
	// LastIndex(".") with dot > 0 also rejects ".png" (empty index).
	dot := strings.LastIndex(idxStr, ".")
	if dot <= 0 || dot == len(idxStr)-1 {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	idx, err := strconv.Atoi(idxStr[:dot])
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(idxStr[dot+1:])
	contentType := "application/octet-stream"
	switch ext {
	case "png":
		contentType = "image/png"
	case "jpg", "jpeg":
		contentType = "image/jpeg"
	}

	path := pdfextract.ImagePath(h.baseDir, articleID, idx, ext)
	body, err := os.ReadFile(path)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Data(http.StatusOK, contentType, body)
}
