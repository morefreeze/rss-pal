package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func interestQuotaContext(uid int) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/interests/generate", nil)
	c.Set("userID", uid)
	return c, w
}
func seedInterestUser(t *testing.T, db *sql.DB) int {
	t.Helper()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('quota-owner','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return uid
}
func TestInterestsHandlerComputeQuota(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		recent, month, dailyLeft, monthLeft int
		allowed                             bool
	}{{"unused", 0, 0, 3, 100, true}, {"daily_boundary", 3, 0, 0, 97, false}, {"monthly_boundary", 0, 100, 3, 0, false}, {"over_limit_clamped", 4, 100, 0, 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			uid := seedInterestUser(t, db)
			for i := 0; i < tc.recent+tc.month; i++ {
				age := "1 hour"
				if i >= tc.recent {
					age = "2 days"
				}
				if _, err := db.Exec(`INSERT INTO user_insights(user_id,status,triggered_by,requested_at,generated_at) VALUES ($1,'done','manual',NOW()-$2::interval,NOW()-$2::interval)`, uid, age); err != nil {
					t.Fatal(err)
				}
			}
			h := &InterestsHandler{userInterestsRepo: repository.NewUserInterestRepository(db)}
			c, _ := interestQuotaContext(uid)
			q, ok := h.computeQuota(c, uid)
			if q.RemainingToday != tc.dailyLeft || q.RemainingMonth != tc.monthLeft || ok != tc.allowed {
				t.Fatalf("quota=%+v allowed=%v", q, ok)
			}
		})
	}
	t.Run("count_failure_denies_admission", func(t *testing.T) {
		db, err := sql.Open("postgres", "postgres://postgres:postgres@127.0.0.1:55432/rsspal_test?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		h := &InterestsHandler{userInterestsRepo: repository.NewUserInterestRepository(db)}
		c, _ := interestQuotaContext(1)
		q, allowed := h.computeQuota(c, 1)
		if allowed {
			t.Fatalf("quota read failed but admission allowed: %+v", q)
		}
	})
}
func localInterestAI(t *testing.T, status int, calls *atomic.Int32) *ai.Summarizer {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == 200 {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"markdown":"complete","recommendations":[]}`}}}})
		} else {
			fmt.Fprint(w, `{"error":{"message":"local injected failure"}}`)
		}
	}))
	t.Cleanup(server.Close)
	return ai.NewSummarizerWithModel("local-test-key", server.URL, "local-test-model")
}
func TestInterestsHandlerRunAsyncManual(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		want string
	}{{"success_preserves_owner", 200, "done"}, {"failure_preserves_owner", 400, "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			db, schema, cleanup := testdb.NewWithSchema(t)
			defer cleanup()
			appDB, closeApp := testdb.NewAsApp(t, schema)
			defer closeApp()
			uid := seedInterestUser(t, db)
			id, err := repository.NewUserInterestRepository(db).InsertPending(uid, "manual", "test")
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			h := &InterestsHandler{userInterestsRepo: repository.NewUserInterestRepository(appDB)}
			h.runAsyncManual(id, uid, localInterestAI(t, tc.code, &calls), "test", nil)
			var status string
			if err := db.QueryRow(`SELECT status FROM user_insights WHERE id=$1`, id).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if calls.Load() == 0 {
				t.Fatal("AI not called")
			}
			if status != tc.want {
				t.Fatalf("background owner=%d id=%d: status=%s want=%s after %d local AI calls", uid, id, status, tc.want, calls.Load())
			}
		})
	}
}
