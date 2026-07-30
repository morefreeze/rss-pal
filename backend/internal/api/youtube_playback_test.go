package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/youtuberelay"
	"github.com/gin-gonic/gin"
)

type fakeYouTubeArticleSource struct {
	article *model.Article
	err     error
	id      int
	userID  int
}

func (s *fakeYouTubeArticleSource) GetForPlayback(_ *gin.Context, id, userID int) (*model.Article, error) {
	s.id = id
	s.userID = userID
	return s.article, s.err
}

type fakeYouTubeRelay struct {
	startReq    youtuberelay.StartRequest
	playback    youtuberelay.Playback
	startErr    error
	manifest    []byte
	openResp    *http.Response
	openErr     error
	openMethod  string
	openTicket  string
	openKind    youtuberelay.StreamKind
	openRange   string
	openIfRange string
}

func (r *fakeYouTubeRelay) Start(_ context.Context, req youtuberelay.StartRequest) (youtuberelay.Playback, error) {
	r.startReq = req
	return r.playback, r.startErr
}

func (r *fakeYouTubeRelay) Manifest(_ string) ([]byte, error) {
	if r.manifest == nil {
		return nil, youtuberelay.ErrSessionNotFound
	}
	return r.manifest, nil
}

func (r *fakeYouTubeRelay) Open(
	_ context.Context,
	method string,
	ticket string,
	kind youtuberelay.StreamKind,
	rangeHeader string,
	ifRange string,
) (*http.Response, error) {
	r.openMethod = method
	r.openTicket = ticket
	r.openKind = kind
	r.openRange = rangeHeader
	r.openIfRange = ifRange
	return r.openResp, r.openErr
}

func youtubeTestRouter(handler *YouTubePlaybackHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", 7)
		c.Next()
	})
	router.POST("/api/articles/:id/youtube-playback", handler.Start)
	router.GET("/api/media/youtube/:ticket/manifest.mpd", handler.Manifest)
	router.GET("/api/media/youtube/:ticket/:kind", handler.Media)
	router.HEAD("/api/media/youtube/:ticket/:kind", handler.Media)
	return router
}

func TestYouTubePlaybackUsesServerOwnedArticleMedia(t *testing.T) {
	source := &fakeYouTubeArticleSource{article: &model.Article{
		ID:        2391,
		URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		MediaURL:  "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
		MediaType: "video/youtube",
	}}
	expiry := time.Date(2026, 7, 30, 18, 0, 0, 0, time.UTC)
	relay := &fakeYouTubeRelay{playback: youtuberelay.Playback{
		Ticket: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Mode:   "dash", Quality: 1080, ExpiresAt: expiry, HasProgressive: true,
	}}
	handler := NewYouTubePlaybackHandler(source, relay)
	router := youtubeTestRouter(handler)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/articles/2391/youtube-playback",
		bytes.NewBufferString(`{"url":"https://evil.example/video"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if source.id != 2391 || source.userID != 7 {
		t.Fatalf("lookup id=%d user=%d", source.id, source.userID)
	}
	if relay.startReq != (youtuberelay.StartRequest{UserID: 7, ArticleID: 2391, VideoID: "dQw4w9WgXcQ"}) {
		t.Fatalf("start request = %+v", relay.startReq)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["manifest_url"] != "/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/manifest.mpd" ||
		body["progressive_url"] != "/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/progressive" ||
		body["quality"] != float64(1080) {
		t.Fatalf("unexpected body: %#v", body)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("evil.example")) {
		t.Fatalf("client URL leaked into response: %s", response.Body.String())
	}
}

func TestYouTubePlaybackRejectsNonYouTubeArticle(t *testing.T) {
	source := &fakeYouTubeArticleSource{article: &model.Article{
		ID:        2391,
		MediaURL:  "https://player.bilibili.com/player.html?bvid=BV1xL3y6cEVv",
		MediaType: "video/bilibili",
	}}
	relay := &fakeYouTubeRelay{}
	router := youtubeTestRouter(NewYouTubePlaybackHandler(source, relay))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/articles/2391/youtube-playback", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if relay.startReq.VideoID != "" {
		t.Fatalf("relay unexpectedly started: %+v", relay.startReq)
	}
}

func TestYouTubePlaybackMapsStartErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "capacity", err: youtuberelay.ErrCapacity, want: http.StatusTooManyRequests},
		{name: "no media", err: youtuberelay.ErrNoCompatibleMedia, want: http.StatusUnprocessableEntity},
		{name: "resolver", err: youtuberelay.ErrResolveFailed, want: http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeYouTubeArticleSource{article: &model.Article{
				ID: 1, MediaType: "video/youtube",
				MediaURL: "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
			}}
			router := youtubeTestRouter(NewYouTubePlaybackHandler(source, &fakeYouTubeRelay{startErr: tc.err}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/articles/1/youtube-playback", nil))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

func TestYouTubePlaybackServesManifestAndFilteredRangeResponse(t *testing.T) {
	relay := &fakeYouTubeRelay{
		manifest: []byte("<MPD/>"),
		openResp: &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Type":   []string{"video/mp4"},
				"Content-Range":  []string{"bytes 10-19/100"},
				"Accept-Ranges":  []string{"bytes"},
				"Content-Length": []string{"10"},
				"Set-Cookie":     []string{"upstream=secret"},
			},
			Body: io.NopCloser(bytes.NewBufferString("0123456789")),
		},
	}
	router := youtubeTestRouter(NewYouTubePlaybackHandler(&fakeYouTubeArticleSource{}, relay))

	manifestResponse := httptest.NewRecorder()
	router.ServeHTTP(manifestResponse, httptest.NewRequest(
		http.MethodGet,
		"/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/manifest.mpd",
		nil,
	))
	if manifestResponse.Code != http.StatusOK ||
		manifestResponse.Header().Get("Content-Type") != "application/dash+xml" ||
		manifestResponse.Body.String() != "<MPD/>" {
		t.Fatalf("manifest status=%d headers=%v body=%s", manifestResponse.Code, manifestResponse.Header(), manifestResponse.Body.String())
	}

	mediaRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/video",
		nil,
	)
	mediaRequest.Header.Set("Range", "bytes=10-19")
	mediaRequest.Header.Set("If-Range", `"etag"`)
	mediaResponse := httptest.NewRecorder()
	router.ServeHTTP(mediaResponse, mediaRequest)
	if mediaResponse.Code != http.StatusPartialContent || mediaResponse.Body.String() != "0123456789" {
		t.Fatalf("media status=%d body=%q", mediaResponse.Code, mediaResponse.Body.String())
	}
	if mediaResponse.Header().Get("Content-Range") != "bytes 10-19/100" ||
		mediaResponse.Header().Get("Set-Cookie") != "" {
		t.Fatalf("unexpected media headers: %v", mediaResponse.Header())
	}
	if relay.openMethod != http.MethodGet || relay.openKind != youtuberelay.StreamVideo ||
		relay.openRange != "bytes=10-19" || relay.openIfRange != `"etag"` {
		t.Fatalf("open call method=%s kind=%s range=%q if-range=%q", relay.openMethod, relay.openKind, relay.openRange, relay.openIfRange)
	}
}

func TestYouTubePlaybackMapsMediaErrorsAndHEAD(t *testing.T) {
	relay := &fakeYouTubeRelay{openErr: youtuberelay.ErrInvalidRange}
	router := youtubeTestRouter(NewYouTubePlaybackHandler(&fakeYouTubeArticleSource{}, relay))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/video",
		nil,
	))
	if response.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", response.Code)
	}

	relay.openErr = nil
	relay.openResp = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Length": []string{"99"}, "Content-Type": []string{"video/mp4"}},
		Body:       io.NopCloser(bytes.NewBufferString("must-not-copy")),
	}
	headResponse := httptest.NewRecorder()
	router.ServeHTTP(headResponse, httptest.NewRequest(
		http.MethodHead,
		"/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/progressive",
		nil,
	))
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 || relay.openMethod != http.MethodHead {
		t.Fatalf("HEAD status=%d body=%q method=%s", headResponse.Code, headResponse.Body.String(), relay.openMethod)
	}
}

func TestYouTubePlaybackMissingArticleIs404(t *testing.T) {
	source := &fakeYouTubeArticleSource{err: errors.New("not found")}
	router := youtubeTestRouter(NewYouTubePlaybackHandler(source, &fakeYouTubeRelay{}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/articles/1/youtube-playback", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}
