package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
)

func TestCaptureRejectsOverlongNoteBeforeCreatingArticle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	articles := newStubArticleRepo()
	h := &BookmarkletHandler{userRepo: &stubUserRepo{token: "note-token", user: &model.User{ID: 1}}, feedRepo: &stubFeedRepo{feed: &model.Feed{ID: 2}}, articleRepo: articles}
	r := gin.New()
	r.POST("/capture", h.Capture)
	b, _ := json.Marshal(map[string]any{"url": "https://example.com/note", "html": "<article><p>" + strings.Repeat("article body ", 30) + "</p></article>", "capture_note": strings.Repeat("记", 2001)})
	req := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer note-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
	}
	if len(articles.created) != 0 {
		t.Fatal("invalid note created an article")
	}
}
