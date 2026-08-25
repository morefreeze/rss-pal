package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestWeeklyHandlerScheduledResponse(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var userID int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('weekly-state-user', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	h := NewWeeklyHandler(repository.NewArticleRepository(db), repository.NewWeeklyDigestRepository(db))
	h.now = func() time.Time { return weeklyTestTime(2026, time.August, 25, 12, 0) }

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/weekly-digest?week=2026-08-24", nil)
	c.Set("userID", userID)
	h.Get(c)

	var body struct {
		Pending               bool                   `json:"pending"`
		GenerationStatus      WeeklyGenerationStatus `json:"generation_status"`
		EstimatedGenerationAt string                 `json:"estimated_generation_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !body.Pending || body.GenerationStatus != WeeklyGenerationScheduled || body.EstimatedGenerationAt != "2026-08-31T05:00:00+08:00" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
