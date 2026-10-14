package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

func noteFixture(t *testing.T) (*httpLeakFixture, string) {
	t.Helper()
	f, cleanup := newHTTPLeakFixture(t)
	t.Cleanup(cleanup)
	token := "capture-note-test-token"
	if _, err := f.privDB.Exec(`UPDATE users SET bookmarklet_token=$1 WHERE id=$2`, token, f.userA); err != nil {
		t.Fatal(err)
	}
	notes := repository.NewCaptureNoteRepository(f.appDB)
	a := repository.NewArticleRepository(f.appDB)
	h := api.NewBookmarkletHandler(repository.NewUserRepository(f.appDB), repository.NewFeedRepository(f.appDB), a).WithCaptureNotes(notes).WithImageBaseDir(t.TempDir())
	f.router.POST("/capture", api.PublicTokenMiddleware(f.appDB, h.ResolveOwner), h.Capture)
	f.router.POST("/capture-pdf", api.PublicTokenMiddleware(f.appDB, h.ResolveOwner), h.CapturePDF)
	nh := api.NewArticleHandler(a, repository.NewArticleUserTagRepository(f.appDB), repository.NewProgressRepository(f.appDB), repository.NewPreferenceRepository(f.appDB), repository.NewHiddenArticleRepository(f.appDB), nil, nil).WithCaptureNotes(notes)
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	g := f.router.Group("/notes-api", auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	g.PATCH("/articles/:id/note", nh.UpdateCaptureNote)
	ch := api.NewClipHandler(repository.NewClipRepository(f.appDB), repository.NewArticleUserTagRepository(f.appDB))
	g.GET("/clip", ch.List)
	return f, token
}

func captureNoteRequest(t *testing.T, f *httpLeakFixture, token, note string, force bool) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"url": "https://example.com/capture-note", "title": "Capture", "html": "<article><p>" + strings.Repeat("Readable article body. ", 30) + "</p></article>", "capture_note": note, "force": force})
	req := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestCaptureNoteHTTPPersistAppendDuplicateAndRead(t *testing.T) {
	f, token := noteFixture(t)
	var id int
	for i, tc := range []struct {
		note, status string
		force        bool
	}{{"第一条", "created", false}, {"第二条", "duplicate", false}, {"第二条", "updated", true}, {" \n ", "duplicate", false}} {
		w := captureNoteRequest(t, f, token, tc.note, tc.force)
		var body struct {
			Status    string `json:"status"`
			ID        int    `json:"article_id"`
			NoteSaved bool   `json:"note_saved"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code >= 300 || body.Status != tc.status || body.NoteSaved != (i < 3) {
			t.Fatalf("capture %d status=%d body=%s", i, w.Code, w.Body.String())
		}
		id = body.ID
	}
	var note, content string
	if err := f.privDB.QueryRow(`SELECT n.capture_note,a.content FROM article_capture_notes n JOIN articles a ON a.id=n.article_id WHERE n.user_id=$1 AND n.article_id=$2`, f.userA, id).Scan(&note, &content); err != nil {
		t.Fatal(err)
	}
	if note != "第一条\n\n第二条" || strings.Contains(content, "第一条") {
		t.Fatalf("note=%q content=%q", note, content)
	}
	detail := f.do(t, http.MethodGet, fmt.Sprintf("/api/articles/%d", id), f.userA)
	var d struct {
		Article struct {
			Note string `json:"capture_note"`
		} `json:"article"`
	}
	json.Unmarshal(detail.Body.Bytes(), &d)
	if detail.Code != 200 || d.Article.Note != note {
		t.Fatalf("detail=%s", detail.Body.String())
	}
	clips := f.do(t, http.MethodGet, "/notes-api/clip", f.userA)
	if clips.Code != 200 || !strings.Contains(clips.Body.String(), `"capture_note":"第一条\n\n第二条"`) {
		t.Fatalf("clips=%s", clips.Body.String())
	}
}

func TestCaptureNoteHTTPPatchETagClearAndAuthorization(t *testing.T) {
	f, _ := noteFixture(t)
	path := fmt.Sprintf("/notes-api/articles/%d/note", f.articleA)
	detailPath := fmt.Sprintf("/api/articles/%d", f.articleA)
	before := f.do(t, http.MethodGet, detailPath, f.userA).Header().Get("ETag")
	patch := func(user int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, user, false))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		uid    int
		body   string
		status int
	}{{f.userA, `{"capture_note":"私密的想法"}`, 200}, {f.userB, `{"capture_note":"forged"}`, 404}, {f.userA, `{}`, 400}, {f.userA, `{"capture_note":null}`, 400}, {f.userA, `{"capture_note":"` + strings.Repeat("字", 10001) + `"}`, 400}} {
		w := patch(tc.uid, tc.body)
		if w.Code != tc.status {
			t.Fatalf("patch status=%d body=%s want %d", w.Code, w.Body.String(), tc.status)
		}
	}
	after := f.do(t, http.MethodGet, detailPath, f.userA)
	if after.Code != 200 || before == after.Header().Get("ETag") || !strings.Contains(after.Body.String(), "私密的想法") {
		t.Fatalf("detail/etag unchanged: %s", after.Body.String())
	}
	if w := patch(f.userA, `{"capture_note":""}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"capture_note":""`) {
		t.Fatalf("clear %d %s", w.Code, w.Body.String())
	}
	var note string
	if err := f.privDB.QueryRow(`SELECT capture_note FROM article_capture_notes WHERE user_id=$1 AND article_id=$2`, f.userA, f.articleA).Scan(&note); err != nil || note != "" {
		t.Fatalf("clear persisted %q err=%v", note, err)
	}
}

func TestCaptureNoteHTTPOverflowRollsBackArticleUpdate(t *testing.T) {
	f, token := noteFixture(t)
	w := captureNoteRequest(t, f, token, "first", false)
	var body struct {
		ID int `json:"article_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.ID == 0 {
		t.Fatalf("initial %s", w.Body.String())
	}
	if _, err := f.privDB.Exec(`UPDATE article_capture_notes SET capture_note=$1 WHERE article_id=$2`, strings.Repeat("字", 10000), body.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`UPDATE articles SET content='original content' WHERE id=$1`, body.ID); err != nil {
		t.Fatal(err)
	}
	w = captureNoteRequest(t, f, token, "cannot fit", true)
	if w.Code != 400 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var content string
	if err := f.privDB.QueryRow(`SELECT content FROM articles WHERE id=$1`, body.ID).Scan(&content); err != nil || content != "original content" {
		t.Fatalf("partial write content=%q err=%v", content, err)
	}
}

func TestCaptureNoteHTTPPDFMultipart(t *testing.T) {
	f, token := noteFixture(t)
	// Existing fixture exercises the digital PDF path through actual extraction tools.
	data, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "digital.pdf"))
	if err != nil {
		t.Fatalf("PDF fixture: %v", err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("url", "https://example.com/note.pdf")
	form.WriteField("capture_note", "PDF 想法")
	file, err := form.CreateFormFile("file", "note.pdf")
	if err != nil {
		t.Fatal(err)
	}
	file.Write(data)
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/capture-pdf", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code >= 300 || !strings.Contains(w.Body.String(), `"note_saved":true`) {
		t.Fatalf("pdf status=%d body=%s", w.Code, w.Body.String())
	}
	var n int
	if err := f.privDB.QueryRow(`SELECT COUNT(*) FROM article_capture_notes WHERE user_id=$1 AND capture_note='PDF 想法'`, f.userA).Scan(&n); err != nil || n != 1 {
		t.Fatalf("PDF note rows=%d err=%v", n, err)
	}
}

func TestCaptureNotePDFOverflowRollsBackContent(t *testing.T) {
	f, token := noteFixture(t)
	if _, err := f.privDB.Exec(`UPDATE articles SET url='https://example.com/overflow.pdf',content='original PDF content' WHERE id=$1`, f.articleA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)`, f.userA, f.articleA, strings.Repeat("字", 10000)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "digital.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("url", "https://example.com/overflow.pdf")
	form.WriteField("capture_note", "overflow")
	file, err := form.CreateFormFile("file", "note.pdf")
	if err != nil {
		t.Fatal(err)
	}
	file.Write(data)
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/capture-pdf", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("pdf status=%d body=%s", w.Code, w.Body.String())
	}
	var content string
	if err := f.privDB.QueryRow(`SELECT content FROM articles WHERE id=$1`, f.articleA).Scan(&content); err != nil || content != "original PDF content" {
		t.Fatalf("overflow committed partial PDF content=%q err=%v", content, err)
	}
}

func rejectCaptureNoteCommit(t *testing.T, f *httpLeakFixture) {
	t.Helper()
	_, err := f.privDB.Exec(`CREATE FUNCTION reject_note_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test commit rejected'; END $$;
 CREATE CONSTRAINT TRIGGER reject_note_commit AFTER INSERT OR UPDATE ON article_capture_notes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_note_commit();`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCaptureNoteCommitFailureNeverReturnsSuccess(t *testing.T) {
	for _, path := range []string{"html", "patch", "pdf"} {
		t.Run(path, func(t *testing.T) {
			f, token := noteFixture(t)
			rejectCaptureNoteCommit(t, f)
			var w *httptest.ResponseRecorder
			switch path {
			case "html":
				w = captureNoteRequest(t, f, token, "must retain draft", false)
			case "patch":
				req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/notes-api/articles/%d/note", f.articleA), strings.NewReader(`{"capture_note":"must retain draft"}`))
				req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, f.userA, false))
				req.Header.Set("Content-Type", "application/json")
				w = httptest.NewRecorder()
				f.router.ServeHTTP(w, req)
			case "pdf":
				w = pdfNoteRequest(t, f, token, "https://example.com/commit.pdf", "must retain draft", "digital.pdf")
			}
			if w.Code < 500 || strings.Contains(w.Body.String(), `"note_saved":true`) || strings.Contains(w.Body.String(), `"capture_note":"must retain draft"`) {
				t.Fatalf("commit failure leaked success status=%d body=%s", w.Code, w.Body.String())
			}
			var n int
			if err := f.privDB.QueryRow(`SELECT count(*) FROM article_capture_notes`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("rolled back note rows=%d err=%v", n, err)
			}
		})
	}
}

func pdfNoteRequest(t *testing.T, f *httpLeakFixture, token, url, note, fixture string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("url", url)
	form.WriteField("capture_note", note)
	file, err := form.CreateFormFile("file", "note.pdf")
	if err != nil {
		t.Fatal(err)
	}
	file.Write(data)
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/capture-pdf", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestCaptureNoteRejectedPDFPreservesImages(t *testing.T) {
	for _, cause := range []string{"overflow", "commit", "success"} {
		t.Run(cause, func(t *testing.T) {
			f, token := noteFixture(t)
			imageDir := t.TempDir()
			h := api.NewBookmarkletHandler(repository.NewUserRepository(f.appDB), repository.NewFeedRepository(f.appDB), repository.NewArticleRepository(f.appDB)).WithCaptureNotes(repository.NewCaptureNoteRepository(f.appDB)).WithImageBaseDir(imageDir)
			f.router.POST("/images-pdf", api.PublicTokenMiddleware(f.appDB, h.ResolveOwner), h.CapturePDF)
			if _, err := f.privDB.Exec(`UPDATE articles SET url='https://example.com/images.pdf',content='original body' WHERE id=$1`, f.articleA); err != nil {
				t.Fatal(err)
			}
			originalPath := filepath.Join(imageDir, "article_images", fmt.Sprint(f.articleA), "0.png")
			if err := os.MkdirAll(filepath.Dir(originalPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(originalPath, []byte("original image bytes"), 0644); err != nil {
				t.Fatal(err)
			}
			if cause == "overflow" {
				if _, err := f.privDB.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)`, f.userA, f.articleA, strings.Repeat("字", 10000)); err != nil {
					t.Fatal(err)
				}
			} else if cause == "commit" {
				rejectCaptureNoteCommit(t, f)
			}
			data, err := os.ReadFile(filepath.Join("..", "pdfextract", "testdata", "mixed.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			form.WriteField("url", "https://example.com/images.pdf")
			form.WriteField("capture_note", "new note")
			file, err := form.CreateFormFile("file", "images.pdf")
			if err != nil {
				t.Fatal(err)
			}
			file.Write(data)
			form.Close()
			req := httptest.NewRequest(http.MethodPost, "/images-pdf", &body)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", form.FormDataContentType())
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, req)
			got, err := os.ReadFile(originalPath)
			if cause == "success" {
				if w.Code != 200 || err != nil || string(got) == "original image bytes" || len(got) < 100 {
					t.Fatalf("successful commit did not publish images: status=%d imageLength=%d err=%v", w.Code, len(got), err)
				}
				return
			}
			expectedStatus := 400
			if cause == "commit" {
				expectedStatus = 500
			}
			if w.Code != expectedStatus {
				t.Fatalf("rejected capture status=%d body=%s", w.Code, w.Body.String())
			}
			if err != nil || string(got) != "original image bytes" {
				t.Fatalf("rejected request overwrote image: length=%d err=%v status=%d", len(got), err, w.Code)
			}
			files, err := os.ReadDir(filepath.Dir(originalPath))
			if err != nil || len(files) != 1 {
				t.Fatalf("rejected request published new images: %v err=%v", files, err)
			}
		})
	}
}
