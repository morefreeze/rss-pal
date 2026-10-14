package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func (h *BookmarkletHandler) WithCaptureNotes(repo *repository.CaptureNoteRepository) *BookmarkletHandler {
	h.noteRepo = repo
	return h
}
func (h *ArticleHandler) WithCaptureNotes(repo *repository.CaptureNoteRepository) *ArticleHandler {
	h.noteRepo = repo
	return h
}

func validateCaptureNote(c *gin.Context, note string) bool {
	if utf8.RuneCountInString(note) > repository.CaptureNoteInputLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "随手记最多 2000 字"})
		return false
	}
	return true
}

func captureNoteError(c *gin.Context, err error) {
	// Request middleware commits 4xx responses unless an error is recorded.
	// PDF extraction may already have written article rows, so this must
	// mark the transaction for rollback on validation failures as well.
	_ = c.Error(err)
	switch {
	case errors.Is(err, repository.ErrCaptureNoteTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": "随手记总长度不能超过 10000 字，请先编辑已有随手记"})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存随手记失败"})
	}
}

func (h *BookmarkletHandler) saveCaptureNote(c *gin.Context, userID, articleID int, note string) (bool, bool) {
	if strings.TrimSpace(note) == "" {
		return false, true
	}
	if h.noteRepo == nil {
		captureNoteError(c, errors.New("capture note repository unavailable"))
		return false, false
	}
	if _, err := h.noteRepo.WithCtx(c).Append(userID, articleID, note); err != nil {
		captureNoteError(c, err)
		return false, false
	}
	return true, true
}

// UpdateCaptureNote replaces this user's private note. A missing/null field is
// invalid rather than an implicit clear; the explicit empty string clears it.
func (h *ArticleHandler) UpdateCaptureNote(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		CaptureNote *string `json:"capture_note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.CaptureNote == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "capture_note 必填"})
		return
	}
	if h.noteRepo == nil {
		captureNoteError(c, errors.New("capture note repository unavailable"))
		return
	}
	saved, err := h.noteRepo.WithCtx(c).Replace(getUserID(c), id, *req.CaptureNote)
	if err != nil {
		captureNoteError(c, err)
		return
	}
	jsonAfterCommit(c, http.StatusOK, gin.H{"capture_note": saved})
}
