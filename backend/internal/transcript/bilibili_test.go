package transcript

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestBilibiliCC_Skips_NonBilibili(t *testing.T) {
	f := &BilibiliCC{}
	got, err := f.Fetch(context.Background(), &model.Article{MediaType: "video/youtube"})
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for non-bilibili, got (%+v, %v)", got, err)
	}
}

func TestBilibiliCC_FetchesTranscript(t *testing.T) {
	viewBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_view.json"))
	playerBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_player_v2.json"))
	subBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_subtitle.json"))

	var subURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/x/web-interface/view"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(viewBytes)
		case strings.Contains(r.URL.Path, "/x/player/v2"):
			body := strings.Replace(string(playerBytes), "//PLACEHOLDER/sub.json", subURL, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/sub.json"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(subBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	subURL = srv.URL + "/sub.json"

	f := &BilibiliCC{
		ViewURLBase:    srv.URL + "/x/web-interface/view?bvid=",
		PlayerV2URLFmt: srv.URL + "/x/player/v2?cid=%d&bvid=%s",
	}

	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/bilibili",
		MediaURL:  "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected Result")
	}
	if !strings.Contains(got.Text, "你好") || !strings.Contains(got.Text, "世界") {
		t.Errorf("transcript missing expected lines: %q", got.Text)
	}
	if got.Source == "" {
		t.Error("source should not be empty")
	}
}

func TestBilibiliCC_NoSubtitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/x/web-interface/view"):
			w.Write([]byte(`{"code":0,"data":{"cid":1,"bvid":"BV1xx411c7mD"}}`))
		case strings.Contains(r.URL.Path, "/x/player/v2"):
			w.Write([]byte(`{"code":0,"data":{"subtitle":{"subtitles":[]}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := &BilibiliCC{
		ViewURLBase:    srv.URL + "/x/web-interface/view?bvid=",
		PlayerV2URLFmt: srv.URL + "/x/player/v2?cid=%d&bvid=%s",
	}
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/bilibili",
		MediaURL:  "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for no-subtitles video, got %+v", got)
	}
}
