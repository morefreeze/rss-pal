package api

import (
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/airouting"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminAIEnforcesAuthorityAndRedacts(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var id int
	if e := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('ai-admin','x',true) RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	h := NewAdminAIHandler(db, airouting.NewStore(db, "secret", "server-key", "https://api.z.ai/api/coding/paas/v4", "glm-5.3-flash"))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", id); c.Set("isAdmin", true) })
	g := r.Group("/", h.RequireAdmin)
	g.GET("", h.Get)
	g.PUT("", h.Save)
	g.POST("models", h.Models)
	do := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "server-key") || strings.Contains(w.Body.String(), "draft-secret") {
			t.Fatal("key leak")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
		return w
	}
	w := do("GET", "/", "", 200)
	var v airouting.View
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	do("PUT", "/", `{"revision":0,"threshold":0}`, 400)
	do("POST", "/models", `{"provider":"anthropic","endpoint":"default"}`, 400)
	do("POST", "/models", `{"provider":"openai","endpoint":"https://evil.invalid","api_key":"draft-secret"}`, 400)
	do("PUT", "/", `{"revision":0,"threshold":10000,"small":{"provider":"zai","model":"glm-5.3-flash"},"large":{"provider":"zai","model":"glm-5.3"}}`, 200)
	do("PUT", "/", `{"revision":0,"threshold":10000}`, 409)
	if _, e := db.Exec(`UPDATE users SET is_admin=false WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	do("GET", "/", "", 403)
	do("PUT", "/", `{}`, 403)
	do("POST", "/models", `{}`, 403)
}
