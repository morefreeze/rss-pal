package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

type shortcutNoteFixture struct {
	db                   *sql.DB
	owner, other, parent int
	router               *gin.Engine
	token                string
}

func newShortcutNoteFixture(t *testing.T) *shortcutNoteFixture {
	t.Helper()
	db, schema, cleanup := testdb.NewWithSchema(t)
	t.Cleanup(cleanup)
	app, cleanapp := testdb.NewAsApp(t, schema)
	t.Cleanup(cleanapp)
	f := &shortcutNoteFixture{db: db}
	for name, id := range map[string]*int{"note-owner": &f.owner, "note-other": &f.other} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	f.parent = seedShortcutSource(t, db, f.owner, "notes", "[First](https://example.com/first) [Second](https://example.com/second) [Root](https://root.example)")
	connections := repository.NewShortcutRepository(db)
	code, _, err := connections.Pair(f.owner)
	if err != nil {
		t.Fatal(err)
	}
	f.token, err = connections.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	f.router = shortcutCaptureRouter(app, db)
	h := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
	g := f.router.Group("/reader", func(c *gin.Context) { uid, _ := strconv.Atoi(c.GetHeader("X-Test-User")); c.Set("userID", uid) }, RLSTxMiddleware(app))
	g.GET("/:id/capture-drafts", h.CaptureDrafts)
	g.POST("/:id/batch_fetch", h.BatchFetch)
	return f
}

func (f *shortcutNoteFixture) request(method, path, body string, user int) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", strconv.Itoa(user))
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *shortcutNoteFixture) capture(url, note string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"url": url, "capture_note": note})
	return f.request("POST", "/api/shortcuts/capture", string(b), f.owner)
}

func (f *shortcutNoteFixture) batch(candidates string) *httptest.ResponseRecorder {
	return f.request("POST", fmt.Sprintf("/reader/%d/batch_fetch", f.parent), `{"candidates":`+candidates+`}`, f.owner)
}

func TestShortcutNoteStagesListsAndTransfersLatestServerState(t *testing.T) {
	f := newShortcutNoteFixture(t)
	if _, err := f.db.Exec(`INSERT INTO article_capture_notes(user_id,article_id,capture_note) VALUES($1,$2,'parent private note')`, f.owner, f.parent); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("想", 1800), strings.Repeat("法", 1800)
	for _, note := range []string{first, first, "  ", second, second} {
		w := f.capture("https://example.com/first#section", note)
		wantAck := strings.TrimSpace(note) != ""
		if w.Code != 202 || !strings.Contains(w.Body.String(), fmt.Sprintf(`"note_saved":%t`, wantAck)) {
			t.Fatalf("stage %d %s", w.Code, w.Body.String())
		}
	}
	if w := f.capture("https://example.com/second", "keep pending"); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	path := fmt.Sprintf("/reader/%d/capture-drafts", f.parent)
	w := f.request("GET", path, "", f.owner)
	var result struct {
		Drafts []struct {
			URL  string `json:"url"`
			Note string `json:"capture_note"`
		} `json:"drafts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := first + "\n\n" + second
	if w.Code != 200 || len(result.Drafts) != 2 || result.Drafts[0].Note != want {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	if w := f.request("GET", path, "", f.other); w.Code != 404 || strings.Contains(w.Body.String(), first) {
		t.Fatalf("foreign list %d %s", w.Code, w.Body.String())
	}
	w = f.batch(`[{"url":"https://example.com/first","title":"First","capture_note":"stale client text"}]`)
	if w.Code != 200 {
		t.Fatalf("batch %d %s", w.Code, w.Body.String())
	}
	var note, body, state string
	if err := f.db.QueryRow(`SELECT n.capture_note,a.content,a.processing_state FROM articles a JOIN article_capture_notes n ON n.article_id=a.id AND n.user_id=$1 WHERE a.parent_article_id=$2`, f.owner, f.parent).Scan(&note, &body, &state); err != nil {
		t.Fatal(err)
	}
	if note != want || body != "" || state != "processing" {
		t.Fatalf("transfer note=%q body=%q state=%q", note, body, state)
	}
	var drafts int
	var parentNote, parentBody string
	if err := f.db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1 AND url='https://example.com/second' AND capture_note='keep pending'`, f.parent).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT n.capture_note,a.content FROM articles a JOIN article_capture_notes n ON n.article_id=a.id WHERE a.id=$1`, f.parent).Scan(&parentNote, &parentBody); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 || parentNote != "parent private note" || !strings.Contains(parentBody, "[First](https://example.com/first)") {
		t.Fatalf("drafts=%d parentNote=%q parentBody=%q", drafts, parentNote, parentBody)
	}
}

func TestShortcutNoteAlreadyFetchedRootAppendsWithoutRequeue(t *testing.T) {
	f := newShortcutNoteFixture(t)
	var child int
	if err := f.db.QueryRow(`INSERT INTO articles(feed_id,parent_article_id,url,title,content,processing_state) SELECT feed_id,id,'https://root.example','Root','original body','done' FROM articles WHERE id=$1 RETURNING id`, f.parent).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO article_capture_notes(user_id,article_id,capture_note) VALUES($1,$2,'existing')`, f.owner, child); err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{"new thought", "new thought", ""} {
		w := f.capture("https://root.example/", note)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"already_fetched"`) || !strings.Contains(w.Body.String(), fmt.Sprintf(`"note_saved":%t`, note != "")) {
			t.Fatalf("existing %d %s", w.Code, w.Body.String())
		}
		if note != "" && !strings.Contains(w.Body.String(), "随手记") {
			t.Fatalf("inaccurate saved message %s", w.Body.String())
		}
	}
	var note, state, body string
	if err := f.db.QueryRow(`SELECT n.capture_note,a.processing_state,a.content FROM articles a JOIN article_capture_notes n ON n.article_id=a.id WHERE a.id=$1`, child).Scan(&note, &state, &body); err != nil {
		t.Fatal(err)
	}
	if note != "existing\n\nnew thought" || state != "done" || body != "original body" {
		t.Fatalf("existing child changed: %q %q %q", note, state, body)
	}
	var drafts int
	if err := f.db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1`, f.parent).Scan(&drafts); err != nil || drafts != 0 {
		t.Fatalf("drafts=%d err=%v", drafts, err)
	}
}

func TestShortcutNoteUnicodeInputAndPendingTotalLimits(t *testing.T) {
	f := newShortcutNoteFixture(t)
	if w := f.capture("https://example.com/first", strings.Repeat("😀", 2001)); w.Code != 400 {
		t.Fatalf("unicode overflow %d %s", w.Code, w.Body.String())
	}
	for i := 0; i < 4; i++ {
		if w := f.capture("https://example.com/first", strings.Repeat(string(rune('甲'+i)), 2000)); w.Code != 202 {
			t.Fatalf("accepted Unicode %d %s", w.Code, w.Body.String())
		}
	}
	if w := f.capture("https://example.com/first", strings.Repeat("末", 2000)); w.Code != 400 || strings.Contains(w.Body.String(), `"note_saved":true`) {
		t.Fatalf("total overflow %d %s", w.Code, w.Body.String())
	}
	var note string
	if err := f.db.QueryRow(`SELECT capture_note FROM article_capture_drafts WHERE article_id=$1`, f.parent).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if len([]rune(note)) != 8006 {
		t.Fatalf("overflow altered pending: %d", len([]rune(note)))
	}
}

func TestShortcutNoteTransferOverflowRollsBackWholeBatch(t *testing.T) {
	f := newShortcutNoteFixture(t)
	for _, url := range []string{"https://example.com/first", "https://root.example/"} {
		if w := f.capture(url, "pending"); w.Code != 202 {
			t.Fatal(w.Body.String())
		}
	}
	var child int
	if err := f.db.QueryRow(`INSERT INTO articles(feed_id,parent_article_id,url,title,content) SELECT feed_id,id,'https://root.example/','Root','' FROM articles WHERE id=$1 RETURNING id`, f.parent).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO article_capture_notes(user_id,article_id,capture_note) VALUES($1,$2,$3)`, f.owner, child, strings.Repeat("字", 10000)); err != nil {
		t.Fatal(err)
	}
	w := f.batch(`[{"url":"https://example.com/first","title":"First"},{"url":"https://root.example/","title":"Root"}]`)
	if w.Code != 400 {
		t.Fatalf("overflow %d %s", w.Code, w.Body.String())
	}
	var drafts, children, notes int
	var enabled sql.NullBool
	if err := f.db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1 AND capture_note='pending'`, f.parent).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, f.parent).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT count(*) FROM article_capture_notes WHERE user_id=$1`, f.owner).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT links_extendable FROM articles WHERE id=$1`, f.parent).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if drafts != 2 || children != 1 || notes != 1 || enabled.Valid {
		t.Fatalf("partial writes drafts=%d children=%d notes=%d enabled=%v", drafts, children, notes, enabled)
	}
}

func TestShortcutNoteCommitFailureRetainsDraftAndDoesNotAcknowledge(t *testing.T) {
	f := newShortcutNoteFixture(t)
	if w := f.capture("https://example.com/first", "retain this"); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if _, err := f.db.Exec(`CREATE FUNCTION reject_shortcut_note_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test commit rejected'; END $$;
CREATE CONSTRAINT TRIGGER reject_shortcut_note_commit AFTER INSERT OR UPDATE ON article_capture_notes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_shortcut_note_commit();`); err != nil {
		t.Fatal(err)
	}
	w := f.batch(`[{"url":"https://example.com/first","title":"First"}]`)
	if w.Code < 500 || strings.Contains(w.Body.String(), `"inserted"`) {
		t.Fatalf("commit failure %d %s", w.Code, w.Body.String())
	}
	var note string
	var children int
	if err := f.db.QueryRow(`SELECT capture_note FROM article_capture_drafts WHERE article_id=$1`, f.parent).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, f.parent).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if note != "retain this" || children != 0 {
		t.Fatalf("lost note or created child %q %d", note, children)
	}
}

func TestShortcutNoteCaptureCommitFailureNeverAcknowledgesSaved(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-%t", existing), func(t *testing.T) {
			f := newShortcutNoteFixture(t)
			if w := f.capture("https://example.com/first", "already saved"); w.Code != 202 {
				t.Fatal(w.Body.String())
			}
			table := "article_capture_drafts"
			if existing {
				if w := f.batch(`[{"url":"https://example.com/first","title":"First"}]`); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				table = "article_capture_notes"
			}
			if _, err := f.db.Exec(`CREATE FUNCTION reject_note_ack() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test commit rejected'; END $$;
CREATE CONSTRAINT TRIGGER reject_note_ack AFTER INSERT OR UPDATE ON ` + table + ` DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_note_ack();`); err != nil {
				t.Fatal(err)
			}
			w := f.capture("https://example.com/first", "must not acknowledge")
			if w.Code < 500 || strings.Contains(w.Body.String(), `"note_saved":true`) {
				t.Fatalf("commit failure acknowledged %d %s", w.Code, w.Body.String())
			}
			var note string
			if err := f.db.QueryRow(`SELECT capture_note FROM `+table+` WHERE user_id=$1`, f.owner).Scan(&note); err != nil {
				t.Fatal(err)
			}
			if note != "already saved" {
				t.Fatalf("committed rejected note %q", note)
			}
		})
	}
}

func TestShortcutNoteTransferMergesIntoExistingRootChild(t *testing.T) {
	f := newShortcutNoteFixture(t)
	if w := f.capture("https://root.example", "pending server note"); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var child int
	if err := f.db.QueryRow(`INSERT INTO articles(feed_id,parent_article_id,url,title,content,processing_state) SELECT feed_id,id,'https://root.example','Root','unchanged','done' FROM articles WHERE id=$1 RETURNING id`, f.parent).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO article_capture_notes(user_id,article_id,capture_note) VALUES($1,$2,'existing private note')`, f.owner, child); err != nil {
		t.Fatal(err)
	}
	w := f.batch(`[{"url":"https://root.example/","title":"Root","capture_note":"client ignored"}]`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":0`) {
		t.Fatalf("root alias duplicated %d %s", w.Code, w.Body.String())
	}
	var note, state, body string
	if err := f.db.QueryRow(`SELECT n.capture_note,a.processing_state,a.content FROM articles a JOIN article_capture_notes n ON n.article_id=a.id WHERE a.id=$1`, child).Scan(&note, &state, &body); err != nil {
		t.Fatal(err)
	}
	if note != "existing private note\n\npending server note" || state != "done" || body != "unchanged" {
		t.Fatalf("transfer existing %q %q %q", note, state, body)
	}
}

func TestShortcutNoteCannotWriteAnotherOwnerSource(t *testing.T) {
	f := newShortcutNoteFixture(t)
	foreign := seedShortcutSource(t, f.db, f.other, "foreign-note", "[Private](https://private.example/foreign)")
	if _, err := f.db.Exec(`INSERT INTO article_capture_drafts(user_id,article_id,url,title,capture_note) VALUES($1,$2,'https://private.example/foreign','Private','other owner private note')`, f.other, foreign); err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", "/api/shortcuts/capture", fmt.Sprintf(`{"url":"https://private.example/foreign","source_article_id":%d,"capture_note":"forged"}`, foreign), f.owner)
	if w.Code != 422 || strings.Contains(w.Body.String(), "other owner private note") {
		t.Fatalf("foreign capture %d %s", w.Code, w.Body.String())
	}
	w = f.request("POST", fmt.Sprintf("/reader/%d/batch_fetch", foreign), `{"candidates":[{"url":"https://private.example/foreign","title":"Private"}]}`, f.owner)
	if w.Code != 404 {
		t.Fatalf("foreign transfer %d %s", w.Code, w.Body.String())
	}
	var note string
	var children int
	if err := f.db.QueryRow(`SELECT capture_note FROM article_capture_drafts WHERE article_id=$1`, foreign).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, foreign).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if note != "other owner private note" || children != 0 {
		t.Fatalf("foreign mutation note=%q children=%d", note, children)
	}
}
