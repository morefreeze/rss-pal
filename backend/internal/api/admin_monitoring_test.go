package api

import (
	"github.com/bytedance/rss-pal/internal/opsmonitor"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestAdminMonitoringDatabaseAuthority(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var id int
	if e := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('monitor-admin','unused',true) RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	handler := NewAdminMonitoringHandler(db, opsmonitor.NewService(db, opsmonitor.DefaultConfig(), nil))
	router := gin.New()
	router.GET("/", func(c *gin.Context) { c.Set("userID", id); c.Set("isAdmin", true) }, handler.Get)
	check := func(url string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != want {
			t.Fatalf("%s got %d %s", url, w.Code, w.Body.String())
		}
	}
	check("/?hours=1", 200)
	check("/?hours=2", 400)
	check("/?limit=51", 400)
	if _, e := db.Exec(`UPDATE users SET is_admin=false WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	check("/", 403)
	if _, e := db.Exec(`UPDATE users SET is_admin=true WHERE id=$1;`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`DROP TABLE operations_events`); e != nil {
		t.Fatal(e)
	}
	check("/", 503)
}
