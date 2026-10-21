package repository

import (
	"database/sql"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/lib/pq"
)

var (
	ErrCaptureSourceNotFound  = errors.New("没有找到包含该链接的来源文章，请回到 RSS Pal 原文章加入待抓取")
	ErrCaptureSourceAmbiguous = errors.New("多篇文章包含该链接，请在来源文章内使用“加入待抓取”")
)

type ArticleCaptureDraft struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	AddedAt     int64  `json:"addedAt"`
	CaptureNote string `json:"capture_note,omitempty"`
}

type CaptureDraftResult struct {
	ParentArticleID int
	SourceTitle     string
	AlreadyFetched  bool
	NoteSaved       bool
}

// Normalize links by the same rule as the existing reader and batch-fetch
// endpoint: fragments/trackers do not create separate pending rows.
// The browser's URL serializer gives a bare origin a root slash. Keep draft
// identity aligned without rewriting previously stored link-set URLs.
func normalizeCaptureDraftURL(raw string) (string, error) {
	canonical, err := rss.NormalizeLinkSetURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return "", err
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

var captureURLPattern = regexp.MustCompile("https?://[^\\s<>\"'`]+")

func captureLinkTitle(body, target string) (string, bool) {
	body = html.UnescapeString(body)
	for _, span := range captureURLPattern.FindAllStringIndex(body, -1) {
		raw := strings.TrimRight(body[span[0]:span[1]], ".,;")
		for {
			canonical, err := normalizeCaptureDraftURL(raw)
			if err == nil && canonical == target {
				title := target
				prefix := body[:span[0]]
				if strings.HasSuffix(prefix, "](") {
					if label := strings.LastIndex(prefix[:len(prefix)-2], "["); label >= 0 {
						if text := strings.TrimSpace(prefix[label+1 : len(prefix)-2]); text != "" && utf8.RuneCountInString(text) <= 500 {
							title = text
						}
					}
				}
				return title, true
			}
			if strings.HasSuffix(raw, ")") && strings.Count(raw, ")") > strings.Count(raw, "(") {
				raw = strings.TrimSuffix(raw, ")")
				continue
			}
			if strings.HasSuffix(raw, "]") {
				raw = strings.TrimSuffix(raw, "]")
				continue
			}
			break
		}
	}
	return "", false
}

// This transaction never fetches a URL or creates a child/standalone article.
// The source row lock serializes enqueue against explicit batch confirmation.
func (r *ArticleRepository) AddShortcutCaptureDraft(userID, sourceID int, pageURL string) (*CaptureDraftResult, error) {
	return r.AddShortcutCaptureDraftWithNote(userID, sourceID, pageURL, "")
}

func (r *ArticleRepository) AddShortcutCaptureDraftWithNote(userID, sourceID int, pageURL, note string) (*CaptureDraftResult, error) {
	if utf8.RuneCountInString(note) > CaptureNoteInputLimit {
		return nil, ErrCaptureNoteTooLong
	}
	note = strings.TrimSpace(note)
	canonical, err := normalizeCaptureDraftURL(pageURL)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(canonical)
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	rows, err := q.Query(`SELECT a.id,a.content FROM articles a JOIN feeds f ON f.id=a.feed_id
 WHERE f.owner_id=$1 AND ($3=0 OR a.id=$3) AND strpos(lower(a.content),lower($2))>0
 AND NOT EXISTS(SELECT 1 FROM hidden_articles h WHERE h.article_id=a.id AND h.user_id=$1)
 ORDER BY a.id DESC`, userID, parsed.Hostname(), sourceID)
	if err != nil {
		return nil, err
	}
	parentID := 0
	for rows.Next() {
		var id int
		var body string
		if err = rows.Scan(&id, &body); err != nil {
			break
		}
		if _, ok := captureLinkTitle(body, canonical); !ok {
			continue
		}
		if parentID != 0 {
			err = ErrCaptureSourceAmbiguous
			break
		}
		parentID = id
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if parentID == 0 {
		return nil, ErrCaptureSourceNotFound
	}
	var sourceTitle, body string
	err = q.QueryRow(`SELECT a.title,a.content FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1 AND f.owner_id=$2 FOR UPDATE OF a`, parentID, userID).Scan(&sourceTitle, &body)
	if err != nil {
		return nil, err
	}
	title, ok := captureLinkTitle(body, canonical)
	if !ok {
		return nil, ErrCaptureSourceNotFound
	}
	result := &CaptureDraftResult{ParentArticleID: parentID, SourceTitle: sourceTitle}
	var childID int
	err = q.QueryRow(`SELECT a.id FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.parent_article_id=$1 AND a.url=ANY($2) AND f.owner_id=$3 ORDER BY a.id LIMIT 1`, parentID, pq.Array(captureDraftURLAliases(canonical)), userID).Scan(&childID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	result.AlreadyFetched = childID != 0
	if result.AlreadyFetched {
		if note != "" {
			_, err = (&CaptureNoteRepository{db: q}).Append(userID, childID, note)
			if err != nil {
				return nil, err
			}
		}
	} else {
		const merged = `CASE WHEN EXCLUDED.capture_note='' THEN article_capture_drafts.capture_note
 WHEN article_capture_drafts.capture_note='' THEN EXCLUDED.capture_note
 WHEN article_capture_drafts.capture_note=EXCLUDED.capture_note
 OR right(article_capture_drafts.capture_note,char_length(EXCLUDED.capture_note)+2)=E'\n\n'||EXCLUDED.capture_note
 THEN article_capture_drafts.capture_note
 ELSE article_capture_drafts.capture_note||E'\n\n'||EXCLUDED.capture_note END`
		var saved string
		err = q.QueryRow(`INSERT INTO article_capture_drafts(user_id,article_id,url,title,capture_note) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(user_id,article_id,url) DO UPDATE SET capture_note=`+merged+`
 WHERE char_length(`+merged+`)<=10000 RETURNING capture_note`, userID, parentID, canonical, title, note).Scan(&saved)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCaptureNoteTooLong
		}
		if err != nil {
			return nil, err
		}
	}
	if err = commit(); err != nil {
		return nil, err
	}
	result.NoteSaved = note != ""
	return result, nil
}

func captureDraftURLAliases(canonical string) []string {
	aliases := []string{canonical}
	u, err := url.Parse(canonical)
	if err == nil && u.Path == "/" {
		u.Path = ""
		aliases = append(aliases, u.String())
	}
	return aliases
}

func authorizeCaptureDrafts(q Querier, userID, articleID int) error {
	var id int
	return q.QueryRow(`SELECT a.id FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1 AND f.owner_id=$2`, articleID, userID).Scan(&id)
}

func (r *ArticleRepository) ListCaptureDrafts(userID, articleID int) ([]ArticleCaptureDraft, []string, error) {
	if err := authorizeCaptureDrafts(r.db, userID, articleID); err != nil {
		return nil, nil, err
	}
	rows, err := r.db.Query(`SELECT url,title,(extract(epoch from added_at)*1000)::bigint,capture_note FROM article_capture_drafts
 WHERE user_id=$1 AND article_id=$2
 ORDER BY added_at,url`, userID, articleID)
	if err != nil {
		return nil, nil, err
	}
	drafts := []ArticleCaptureDraft{}
	for rows.Next() {
		var d ArticleCaptureDraft
		if err = rows.Scan(&d.URL, &d.Title, &d.AddedAt, &d.CaptureNote); err != nil {
			break
		}
		drafts = append(drafts, d)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	fetched, err := r.GetChildURLs(articleID)
	if err != nil {
		return nil, nil, err
	}
	fetchedSet := make(map[string]struct{}, len(fetched))
	normalizedFetched := make([]string, 0, len(fetched))
	for _, raw := range fetched {
		canonical, normalizeErr := normalizeCaptureDraftURL(raw)
		if normalizeErr != nil {
			continue
		}
		fetchedSet[canonical] = struct{}{}
		normalizedFetched = append(normalizedFetched, canonical)
	}
	pending := make([]ArticleCaptureDraft, 0, len(drafts))
	for _, draft := range drafts {
		if _, exists := fetchedSet[draft.URL]; !exists {
			pending = append(pending, draft)
		}
	}
	return pending, normalizedFetched, nil
}

func (r *ArticleRepository) DeleteCaptureDrafts(userID, articleID int, urls []string) error {
	if err := authorizeCaptureDrafts(r.db, userID, articleID); err != nil {
		return err
	}
	normalized := make([]string, 0, len(urls))
	for _, raw := range urls {
		canonical, err := normalizeCaptureDraftURL(raw)
		if err != nil {
			continue
		}
		normalized = append(normalized, canonical)
	}
	_, err := r.db.Exec(`DELETE FROM article_capture_drafts WHERE user_id=$1 AND article_id=$2 AND url=ANY($3)`, userID, articleID, pq.Array(normalized))
	return err
}
