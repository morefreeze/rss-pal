package api_test

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestOPMLTitlePersistenceAndOwnerExport(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), nil)
	feeds := api.NewFeedHandler(repository.NewFeedRepository(f.appDB), repository.NewArticleRepository(f.appDB), "")
	r := gin.New()
	r.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	r.POST("/feeds", feeds.Create)
	r.GET("/feeds/export/opml", feeds.ExportOPML)
	request := func(method, path, body string, user int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, user, false))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if _, err := f.privDB.Exec(`INSERT INTO feeds(url,title,owner_id,feed_type) VALUES ('bookmarklet://user/opml','clip',$1,'rss'), ('https://opml.example/oneoff','oneoff',$1,'oneoff_link_set'), ('https://opml.example/html','HTML',$1,'html')`, f.userA); err != nil {
		t.Fatal(err)
	}
	title := "中文 & <AI> \"Test\"\nSecond"
	body, _ := json.Marshal(map[string]string{"url": "https://opml.example/feed?a=1&b=2", "feed_type": "rss", "title": title})
	w := request(http.MethodPost, "/feeds", string(body), f.userA)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var feed model.Feed
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	if feed.Title != title {
		t.Fatalf("title=%q, want %q", feed.Title, title)
	}
	w = request(http.MethodGet, "/feeds/export/opml", "", f.userA)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	var document struct {
		Body struct {
			Outlines []struct {
				Title string `xml:"title,attr"`
				URL   string `xml:"xmlUrl,attr"`
			} `xml:"outline"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "oneoff") || strings.Contains(w.Body.String(), "bookmarklet:") {
		t.Fatal("export contains nonportable internal source")
	}
	if !strings.Contains(w.Body.String(), `type="html"`) {
		t.Fatal("html type lost")
	}
	var found bool
	for _, outline := range document.Body.Outlines {
		if !strings.HasPrefix(outline.URL, "https://") && !strings.HasPrefix(outline.URL, "http://") {
			t.Fatalf("nonportable URL: %q", outline.URL)
		}
		if outline.URL == feed.URL {
			found = true
			if outline.Title != title {
				t.Fatalf("round trip title=%q", outline.Title)
			}
		}
	}
	if !found {
		t.Fatal("created subscription missing from export")
	}
	stored, err := repository.NewFeedRepository(f.privDB).GetByID(feed.ID)
	if err != nil || stored.Title != title {
		t.Fatalf("stored title: %+v %v", stored, err)
	}
	if err := repository.NewFeedRepository(f.privDB).UpdateTitle(feed.ID, "upstream title"); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.NewFeedRepository(f.privDB).GetByID(feed.ID)
	if err != nil || stored.Title != title {
		t.Fatalf("custom title overwritten: %+v %v", stored, err)
	}
	other := request(http.MethodGet, "/feeds/export/opml", "", f.userB)
	if strings.Contains(other.Body.String(), "opml.example") {
		t.Fatal("export leaked another user's feed")
	}
	for _, outline := range document.Body.Outlines {
		payload, _ := json.Marshal(map[string]string{"url": outline.URL, "title": outline.Title})
		imported := request(http.MethodPost, "/feeds", string(payload), f.userB)
		if imported.Code != http.StatusCreated {
			t.Fatalf("reimport: %d %s", imported.Code, imported.Body.String())
		}
	}
	duplicate := request(http.MethodPost, "/feeds", string(body), f.userA)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d", duplicate.Code)
	}
}
