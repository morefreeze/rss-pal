package api

import (
	"context"
	"github.com/bytedance/rss-pal/internal/englife"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnglifeAdminAuthorityAndSafeResponses(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var id int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('englife-api','unused',false) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	svc, _ := englife.NewService(englife.NewSQLStore(db), strings.Repeat("a", 43)+"=")
	h := NewEnglifeHandler(db, svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", id); c.Set("isAdmin", true) })
	r.GET("/", h.Get)
	r.POST("/pair", h.Pair)
	r.POST("/complete", h.Complete)
	r.DELETE("/", h.Disconnect)
	check := func(method, path, body string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	check("GET", "/", "", 403)
	db.Exec(`UPDATE users SET is_admin=true WHERE id=$1`, id)
	check("GET", "/", "", 200)
	check("POST", "/complete", `{"token":"invalid","cookies":[]}`, 400)
	p, err := svc.Pair(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"token":"` + p.Token + `","cookies":[{"name":"SID","value":"private-value","domain":"google.com","path":"/"}]}`
	out := check("POST", "/complete", body, 409)
	if strings.Contains(out, "private-value") || strings.Contains(out, p.Token) {
		t.Fatal("secret in response")
	}
	out = check("GET", "/", "", 200)
	if strings.Contains(out, "cookies") || strings.Contains(out, "token") {
		t.Fatal(out)
	}
	check("DELETE", "/", "", 200)
	check("POST", "/complete", body, 400)
}
