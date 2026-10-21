// Package explorecontent caches bounded, direct public-page extraction after API authorization.
package explorecontent

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type ArticleContentService struct {
	db     *sql.DB
	client *http.Client
}

func NewArticleContentService(db *sql.DB, client *http.Client) *ArticleContentService {
	if client == nil {
		client = httpx.NewClient(22 * time.Second)
	}
	return &ArticleContentService{db: db, client: client}
}

// Overlay reads only; expired leases become retryable failures even if the owning process died.
func (s *ArticleContentService) Overlay(ctx context.Context, d *repository.ExploreArticleDetail) error {
	d.ContentStatus = "feed"
	d.ContentError = ""
	d.ContentRetryAt = nil
	var body, status, message string
	var expiry, retry sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT content,status,last_error,lease_expires_at,retry_at FROM explore_article_content WHERE article_id=$1 AND source_url=$2`, d.ID, d.URL).Scan(&body, &status, &message, &expiry, &retry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "fetching" && (!expiry.Valid || !expiry.Time.After(time.Now())) {
		status = "failed"
		message = "正文抓取中断，请重试"
		retry = sql.NullTime{Time: time.Now(), Valid: true}
	}
	if status == "fetched" && d.Content != nil && utf8.RuneCountInString(body) < utf8.RuneCountInString(*d.Content) {
		return nil
	}
	d.ContentStatus = status
	d.ContentError = message
	if retry.Valid {
		d.ContentRetryAt = &retry.Time
	}
	if body != "" && (d.Content == nil || utf8.RuneCountInString(body) >= utf8.RuneCountInString(*d.Content)) {
		d.Content = &body
	}
	return nil
}

func (s *ArticleContentService) Fetch(ctx context.Context, d *repository.ExploreArticleDetail, charge func() (func(), error)) error {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	var attempts int
	var feedHash string
	var currentFeed sql.NullString
	err := s.db.QueryRowContext(ctx, `INSERT INTO explore_article_content(article_id,source_url,status,lease_token,lease_expires_at)
 SELECT id,url,'fetching',$3,clock_timestamp()+INTERVAL '60 seconds' FROM explore_articles WHERE id=$1 AND url=$2
 ON CONFLICT(article_id) DO UPDATE SET source_url=EXCLUDED.source_url,status='fetching',lease_token=EXCLUDED.lease_token,lease_expires_at=EXCLUDED.lease_expires_at,last_error='',retry_at=NULL,
 content=CASE WHEN explore_article_content.source_url=EXCLUDED.source_url THEN explore_article_content.content ELSE '' END,
 attempts=CASE WHEN explore_article_content.source_url=EXCLUDED.source_url THEN explore_article_content.attempts+1 ELSE 1 END
 WHERE explore_article_content.source_url<>EXCLUDED.source_url OR (explore_article_content.status='fetching' AND explore_article_content.lease_expires_at<=clock_timestamp()) OR (explore_article_content.status='failed' AND explore_article_content.retry_at<=clock_timestamp()) OR (explore_article_content.status='fetched' AND char_length(explore_article_content.content)<(SELECT char_length(COALESCE(content,'')) FROM explore_articles WHERE id=$1))
 RETURNING attempts,(SELECT md5(COALESCE(content,'')) FROM explore_articles WHERE id=$1),(SELECT content FROM explore_articles WHERE id=$1)`, d.ID, d.URL, token).Scan(&attempts, &feedHash, &currentFeed)
	if errors.Is(err, sql.ErrNoRows) {
		return s.Overlay(ctx, d)
	}
	if err != nil {
		return err
	}
	if currentFeed.Valid {
		d.Content = &currentFeed.String
	}
	fail := func(message string) error {
		// Use a fresh bounded context so request cancellation cannot strand the lease.
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		seconds := 30 * (1 << min(attempts-1, 6))
		retryAt := time.Now().Add(time.Duration(seconds) * time.Second)
		_, e := s.db.ExecContext(saveCtx, `UPDATE explore_article_content SET status='failed',last_error=$4,retry_at=$5,lease_expires_at=NULL,updated_at=clock_timestamp() WHERE article_id=$1 AND source_url=$2 AND lease_token=$3 AND status='fetching'`, d.ID, d.URL, token, message, retryAt)
		if e != nil {
			return e
		}
		var latest sql.NullString
		if readErr := s.db.QueryRowContext(saveCtx, `SELECT content FROM explore_articles WHERE id=$1 AND url=$2`, d.ID, d.URL).Scan(&latest); readErr == nil && latest.Valid {
			d.Content = &latest.String
		}
		return s.Overlay(saveCtx, d)
	}
	if charge != nil {
		release, e := charge()
		if e != nil {
			// Admission belongs to this caller; it must not block other readers globally.
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, releaseErr := s.db.ExecContext(releaseCtx, `DELETE FROM explore_article_content WHERE article_id=$1 AND source_url=$2 AND lease_token=$3 AND status='fetching'`, d.ID, d.URL, token)
			if releaseErr == nil {
				_ = s.Overlay(releaseCtx, d)
			}
			releaseCancel()
			return e
		}
		defer release()
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	response, e := httpx.FetchBounded(fetchCtx, s.client, d.URL, nil, 4<<20)
	if e != nil {
		return fail("原文暂时无法读取，请稍后重试")
	}
	kind := strings.ToLower(response.Header.Get("Content-Type"))
	if kind != "" && !strings.Contains(kind, "html") && !strings.Contains(kind, "text/plain") {
		return fail("原文格式暂不支持")
	}
	result, e := (&rss.ContentFetcher{}).FetchContentWithMetadataFromReaderURL(bytes.NewReader(response.Body), response.FinalURL)
	if e != nil {
		return fail("原文解析失败，请稍后重试")
	}
	body := strings.TrimSpace(result.Content)
	old := ""
	if d.Content != nil {
		old = *d.Content
	}
	if !acceptableBody(body, old) {
		return fail("未能提取完整正文，已保留订阅源内容")
	}
	saved, e := s.db.ExecContext(fetchCtx, `UPDATE explore_article_content cache SET content=$4,status='fetched',last_error='',retry_at=NULL,lease_expires_at=NULL,updated_at=clock_timestamp() WHERE article_id=$1 AND source_url=$2 AND lease_token=$3 AND status='fetching' AND lease_expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM explore_articles a WHERE a.id=cache.article_id AND a.url=cache.source_url AND md5(COALESCE(a.content,''))=$5)`, d.ID, d.URL, token, body, feedHash)
	if e != nil {
		return e
	}
	if n, _ := saved.RowsAffected(); n == 0 {
		return fail("订阅源正文已更新，请重试读取原文")
	}
	return s.Overlay(ctx, d)
}
func acceptableBody(body, old string) bool {
	n := utf8.RuneCountInString(body)
	if n < 100 || n < utf8.RuneCountInString(old) {
		return false
	}
	lower := strings.ToLower(body)
	for _, stub := range []string{"just a moment...", "verify you are human", "enable javascript and cookies", "checking your browser", "access denied", "subscribe to continue", "sign in to continue", "log in to continue", "please enable cookies"} {
		if strings.Contains(lower, stub) && n < 2000 {
			return false
		}
	}
	return true
}
