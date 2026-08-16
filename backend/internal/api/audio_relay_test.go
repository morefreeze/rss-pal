package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeAudioMediaSource struct {
	byID map[int]string
}

func (s *fakeAudioMediaSource) GetMediaByID(id int) (string, string, error) {
	if url, ok := s.byID[id]; ok {
		return url, "audio/mpeg", nil
	}
	return "", "", errNotFoundForTest
}

var errNotFoundForTest = &audioRelayError{"not found"}

// rangeAudioUpstream serves a fixed payload with byte-range support,
// mirroring how podcast CDNs behave.
func rangeAudioUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	payload := strings.Repeat("ID3-audiodata-", 256) // 4KB
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ep.mp3" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Errorf("upstream: expected User-Agent to be forwarded")
		}
		total := int64(len(payload))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "audio/mpeg")
		rng := r.Header.Get("Range")
		if rng == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload))
			return
		}
		var start, end int64
		if _, err := parseRangeForTest(rng, total, &start, &end); err != nil {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(total, 10))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(payload[start : end+1]))
	}))
}

func parseRangeForTest(rng string, total int64, start, end *int64) (bool, error) {
	if !strings.HasPrefix(rng, "bytes=") {
		return false, &audioRelayError{"bad range"}
	}
	parts := strings.SplitN(strings.TrimPrefix(rng, "bytes="), "-", 2)
	if len(parts) != 2 {
		return false, &audioRelayError{"bad range"}
	}
	var err error
	if *start, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return false, err
	}
	if parts[1] == "" {
		*end = total - 1
	} else if *end, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return false, err
	}
	if *start < 0 || *start > *end || *start >= total {
		return false, &audioRelayError{"range out of bounds"}
	}
	if *end >= total {
		*end = total - 1
	}
	return true, nil
}

func newAudioRelayRouter(t *testing.T, source AudioMediaSource) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAudioRelayHandler(source, "")
	router.GET("/api/media/audio/:id", handler.Serve)
	router.HEAD("/api/media/audio/:id", handler.Serve)
	return router
}

func TestAudioRelay_FullBodyPassthrough(t *testing.T) {
	upstream := rangeAudioUpstream(t)
	defer upstream.Close()

	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{2596: upstream.URL + "/ep.mp3"}})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/media/audio/2596", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, "audio/mpeg") {
		t.Fatalf("content-type = %q", ct)
	}
	if got := resp.Body.Len(); got != 4096 {
		t.Fatalf("body length = %d, want 4096", got)
	}
	if cc := resp.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("cache-control = %q", cc)
	}
	if resp.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("Accept-Ranges not forwarded")
	}
}

func TestAudioRelay_RangePassthrough(t *testing.T) {
	upstream := rangeAudioUpstream(t)
	defer upstream.Close()

	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{1: upstream.URL + "/ep.mp3"}})

	req := httptest.NewRequest(http.MethodGet, "/api/media/audio/1", nil)
	req.Header.Set("Range", "bytes=100-199")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.Code)
	}
	if cr := resp.Header().Get("Content-Range"); !strings.HasPrefix(cr, "bytes 100-199/") {
		t.Fatalf("content-range = %q", cr)
	}
	if resp.Body.Len() != 100 {
		t.Fatalf("body length = %d, want 100", resp.Body.Len())
	}
}

func TestAudioRelay_HEADNoBody(t *testing.T) {
	upstream := rangeAudioUpstream(t)
	defer upstream.Close()

	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{1: upstream.URL + "/ep.mp3"}})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodHead, "/api/media/audio/1", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if resp.Body.Len() != 0 {
		t.Fatalf("HEAD returned %d bytes", resp.Body.Len())
	}
}

func TestAudioRelay_MissingArticleOrMedia(t *testing.T) {
	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{2: ""}})

	for _, path := range []string{"/api/media/audio/999", "/api/media/audio/2", "/api/media/audio/abc", "/api/media/audio/0"} {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
		if resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 404/400", path, resp.Code)
		}
	}
}

func TestAudioRelay_RejectsNonAudioContentType(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>not a podcast</html>"))
	}))
	defer upstream.Close()

	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{3: upstream.URL + "/page"}})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/media/audio/3", nil))
	if resp.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.Code)
	}
}

func TestAudioRelay_PrivateUpstreamBlocked(t *testing.T) {
	// The SSRF guard must refuse to fetch RFC1918 targets even though the
	// URL was stored in the DB by a (hypothetically malicious) feed.
	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{4: "http://192.168.1.1/secret.mp3"}})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/media/audio/4", nil))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Code)
	}
}

func TestAudioRelay_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	router := newAudioRelayRouter(t, &fakeAudioMediaSource{byID: map[int]string{5: upstream.URL + "/x.mp3"}})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/media/audio/5", nil))
	if resp.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.Code)
	}
}
