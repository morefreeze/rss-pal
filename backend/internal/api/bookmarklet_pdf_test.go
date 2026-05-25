package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
)

// ----- in-memory stub repos -----
//
// These stubs implement the package-private bookmarkletUserRepo /
// bookmarkletFeedRepo / bookmarkletArticleRepo interfaces declared in
// bookmarklet.go. They let the PDF capture handlers be exercised end-
// to-end without a real database. They're intentionally minimal: just
// enough state to satisfy the handlers' control flow.

type stubUserRepo struct {
	token string
	user  *model.User
}

func (s *stubUserRepo) GetByBookmarkletToken(token string) (*model.User, error) {
	if token != s.token {
		return nil, nil
	}
	return s.user, nil
}

type stubFeedRepo struct {
	feed *model.Feed
	err  error
}

func (s *stubFeedRepo) GetOrCreateClipFeed(ownerID int) (*model.Feed, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.feed, nil
}

type stubArticleRepo struct {
	nextID         int32 // atomic so concurrent writes are safe
	byOwnerAndURL  map[string]*model.Article
	created        []*model.Article
	contentUpdates map[int]string
	titleUpdates   map[int]string
	summaryClears  map[int]bool
	pdfStubs       []int
	failedReasons  map[int]string
	readyContents  map[int]string
}

func newStubArticleRepo() *stubArticleRepo {
	return &stubArticleRepo{
		byOwnerAndURL:  map[string]*model.Article{},
		contentUpdates: map[int]string{},
		titleUpdates:   map[int]string{},
		summaryClears:  map[int]bool{},
		failedReasons:  map[int]string{},
		readyContents:  map[int]string{},
	}
}

func (s *stubArticleRepo) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	key := fmt.Sprintf("%d|%s", ownerID, exactURL)
	a, ok := s.byOwnerAndURL[key]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (s *stubArticleRepo) Create(a *model.Article) error {
	a.ID = int(atomic.AddInt32(&s.nextID, 1))
	s.created = append(s.created, a)
	return nil
}

func (s *stubArticleRepo) UpdateContent(id int, content string, wordCount, readingMinutes int) error {
	s.contentUpdates[id] = content
	return nil
}

func (s *stubArticleRepo) UpdateTitle(id int, title string) error {
	s.titleUpdates[id] = title
	return nil
}

func (s *stubArticleRepo) UpdateSummary(id int, brief, detailed string) error {
	s.summaryClears[id] = (brief == "" && detailed == "")
	return nil
}

func (s *stubArticleRepo) CreatePDFStub(a *model.Article) error {
	a.ID = int(atomic.AddInt32(&s.nextID, 1))
	a.ProcessingState = "processing"
	a.Content = ""
	a.IsClip = true
	s.pdfStubs = append(s.pdfStubs, a.ID)
	return nil
}

func (s *stubArticleRepo) UpdateContentAndMarkReady(id int, content string, wordCount, readingMinutes int) error {
	s.readyContents[id] = content
	return nil
}

func (s *stubArticleRepo) MarkPDFFailed(id int, msg string) error {
	s.failedReasons[id] = msg
	return nil
}

// newTestBookmarkletHandlerForPDF wires a handler against stubbed repos.
// The "test-token" maps to a fixed user (id=42); requests authenticated
// with any other token get 401, mirroring the real auth flow.
//
// imageBaseDir is set to t.TempDir() so the handlers can write images
// without polluting the source tree.
func newTestBookmarkletHandlerForPDF(t *testing.T) (*BookmarkletHandler, *stubArticleRepo) {
	t.Helper()
	user := &model.User{ID: 42, Username: "test"}
	feed := &model.Feed{ID: 7, Title: "⭐ 网摘", FeedType: "clip"}
	articleRepo := newStubArticleRepo()
	h := &BookmarkletHandler{
		userRepo:     &stubUserRepo{token: "test-token", user: user},
		feedRepo:     &stubFeedRepo{feed: feed},
		articleRepo:  articleRepo,
		imageBaseDir: t.TempDir(),
	}
	return h, articleRepo
}

// ----- tests -----

func TestCapturePDF_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf", h.CapturePDF)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("url", "https://example.com/foo.pdf")
	w.Close()

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf", body)
	// Note: no Authorization header → handler should 401 before parsing form.
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapturePDF_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf", h.CapturePDF)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, _ := w.CreateFormFile("file", "x.pdf")
	fw.Write([]byte("%PDF-1.4\n"))
	w.Close()

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapturePDF_MissingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf", h.CapturePDF)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("url", "https://example.com/foo.pdf")
	w.Close()

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCapturePDF_Success exercises the full sync (digital PDF) path:
// multipart form → handler → processPDFCapture → stub repos.
// Skips when the fixture or poppler-utils isn't available so the test
// stays useful on CI without breaking dev loops.
func TestCapturePDF_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pdfBytes, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "digital.pdf"))
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	if !pdfextractToolingAvailable() {
		t.Skip("pdfextract tooling (poppler-utils) not on PATH; skipping integration")
	}

	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf", h.CapturePDF)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("url", "https://example.com/digital.pdf")
	_ = w.WriteField("title", "Digital test")
	fw, _ := w.CreateFormFile("file", "digital.pdf")
	fw.Write(pdfBytes)
	w.Close()

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created article, got %d", len(repo.created))
	}
	if _, ok := repo.readyContents[repo.created[0].ID]; !ok {
		t.Errorf("expected UpdateContentAndMarkReady on created article %d", repo.created[0].ID)
	}
}

func TestPdfStatusToHTTP(t *testing.T) {
	cases := []struct {
		status string
		want   int
	}{
		{"created", http.StatusCreated},
		{"updated", http.StatusOK},
		{"processing", http.StatusAccepted},
		{"unknown", http.StatusCreated}, // default
		{"", http.StatusCreated},
	}
	for _, tc := range cases {
		if got := pdfStatusToHTTP(tc.status); got != tc.want {
			t.Errorf("pdfStatusToHTTP(%q) = %d, want %d", tc.status, got, tc.want)
		}
	}
}

func TestFilenameFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://example.com/foo/bar.pdf", "bar"},
		{"https://example.com/foo/bar.PDF", "bar"},
		{"https://example.com/foo/bar.pdf?token=x", "bar"},
		{"https://example.com/foo/bar.pdf#page=3", "bar"},
		{"https://example.com/papers/knuth-1980.pdf", "knuth-1980"},
		// filepath.Base strips the trailing slash and returns the last
		// non-empty segment, so a bare-host URL collapses to its host —
		// the caller (processPDFCapture) then falls through to the
		// normalized URL since the title's still useful for display.
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
	}
	for _, tc := range cases {
		if got := filenameFromURL(tc.in); got != tc.want {
			t.Errorf("filenameFromURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// pdfextractToolingAvailable reports whether the poppler-utils binaries
// pdfextract.ExtractFast shells out to are on PATH. Used to gate the
// success-path integration tests so they cleanly skip on machines
// without poppler installed instead of failing with confusing errors.
func pdfextractToolingAvailable() bool {
	// Probe the cheapest tool first — pdfinfo is required for title
	// extraction and is part of the same poppler package as the others
	// we use, so if it's missing the rest are too.
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return false
	}
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

// Compile-time assertion that the stubs satisfy the handler's interfaces
// — catches drift if the interfaces gain methods.
var _ bookmarkletArticleRepo = (*stubArticleRepo)(nil)
var _ bookmarkletUserRepo = (*stubUserRepo)(nil)
var _ bookmarkletFeedRepo = (*stubFeedRepo)(nil)
