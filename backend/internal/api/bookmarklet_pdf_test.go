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
	byID           map[int]*model.Article
	created        []*model.Article
	contentUpdates map[int]string
	titleUpdates   map[int]string
	summaryClears  map[int]bool
	pdfStubs       []int
	failedReasons  map[int]string
	readyContents  map[int]string
	resetCalls     []int
}

func newStubArticleRepo() *stubArticleRepo {
	return &stubArticleRepo{
		byOwnerAndURL:  map[string]*model.Article{},
		byID:           map[int]*model.Article{},
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

func (s *stubArticleRepo) ResetPDFToProcessing(id int) error {
	s.resetCalls = append(s.resetCalls, id)
	if a, ok := s.byID[id]; ok {
		a.ProcessingState = "processing"
		a.ProcessingError = ""
	}
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

func TestCapturePDFURL_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url",
		bytes.NewBufferString(`{"url":"https://example.com/x.pdf"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestCapturePDFURL_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url",
		bytes.NewBufferString(`{"url":""}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestCapturePDFURL_NonPDFContentType verifies the handler rejects URLs
// whose response is neither application/pdf nor obviously a .pdf file.
// Without this guard a typo'd URL would happily be fed to pdfextract,
// which would then waste CPU returning a corrupt-PDF error.
func TestCapturePDFURL_NonPDFContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	body := bytes.NewBufferString(`{"url":"` + srv.URL + `/page.html"}`)
	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCapturePDFURL_FetchAndProcess covers the happy path: server-side
// fetch of a valid PDF, then full sync extraction into a stub repo.
func TestCapturePDFURL_FetchAndProcess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pdfBytes, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "digital.pdf"))
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	if !pdfextractToolingAvailable() {
		t.Skip("pdfextract tooling (poppler-utils) not on PATH; skipping integration")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdfBytes)
	}))
	defer srv.Close()

	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	body := bytes.NewBufferString(`{"url":"` + srv.URL + `/foo.pdf"}`)
	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created article, got %d", len(repo.created))
	}
}

// TestCapturePDFURL_FetchError covers the bad-gateway path: upstream
// returns a non-200, handler should surface 502 rather than 500.
func TestCapturePDFURL_FetchError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	body := bytes.NewBufferString(`{"url":"` + srv.URL + `/missing.pdf"}`)
	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestProcessPDFCapture_ReQueueExisting covers the scanned-PDF re-capture
// path: when the user re-bookmarks a PDF whose article already exists,
// the handler must flip processing_state back to 'processing' so the OCR
// worker re-picks it up. Previously this called MarkPDFFailed which left
// the row in 'failed' and silently dropped the retry.
func TestProcessPDFCapture_ReQueueExisting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pdfBytes, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "scanned.pdf"))
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	if !pdfextractToolingAvailable() {
		t.Skip("pdfextract tooling (poppler-utils) not on PATH; skipping integration")
	}

	h, repo := newTestBookmarkletHandlerForPDF(t)

	// Pre-populate an existing article for the same normalized URL,
	// already in 'failed' state with a stale error message (simulating
	// a prior OCR attempt that timed out).
	const captureURL = "https://example.com/scanned.pdf"
	existing := &model.Article{
		ID:              101,
		FeedID:          7,
		Title:           "old title",
		URL:             captureURL,
		IsClip:          true,
		ProcessingState: "failed",
		ProcessingError: "previous OCR failed",
	}
	repo.byOwnerAndURL[fmt.Sprintf("%d|%s", 42, captureURL)] = existing
	repo.byID[existing.ID] = existing

	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf", h.CapturePDF)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("url", captureURL)
	fw, _ := w.CreateFormFile("file", "scanned.pdf")
	fw.Write(pdfBytes)
	w.Close()

	req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (processing), got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.resetCalls) != 1 || repo.resetCalls[0] != existing.ID {
		t.Errorf("expected ResetPDFToProcessing(%d) once, got %v", existing.ID, repo.resetCalls)
	}
	if existing.ProcessingState != "processing" {
		t.Errorf("expected processing_state='processing', got %q", existing.ProcessingState)
	}
	if existing.ProcessingError != "" {
		t.Errorf("expected processing_error='', got %q", existing.ProcessingError)
	}
	// Regression guard: must NOT have called MarkPDFFailed on the re-queue path.
	if _, ok := repo.failedReasons[existing.ID]; ok {
		t.Errorf("re-queue path must not call MarkPDFFailed; failedReasons=%v", repo.failedReasons)
	}
}

// TestCapturePDFURL_RejectsNonHTTPScheme verifies SSRF mitigation: the
// handler must refuse file://, gopher://, data:, etc. before any HTTP
// fetch is attempted, returning 400.
func TestCapturePDFURL_RejectsNonHTTPScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture-pdf-url", h.CapturePDFURL)

	cases := []string{
		`{"url":"file:///etc/passwd"}`,
		`{"url":"gopher://internal.svc/x.pdf"}`,
		`{"url":"ftp://example.com/x.pdf"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest("POST", "/api/bookmarklet/capture-pdf-url",
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s expected 400, got %d body=%s", body, rec.Code, rec.Body.String())
		}
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
