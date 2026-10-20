package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// EnqueueCapture serializes shortcut shares for an owner, including first use
// before their clip feed exists. Clip rows intentionally have no URL uniqueness
// constraint (the bookmarklet supports keeping multiple versions).
func (r *ArticleRepository) EnqueueCapture(ownerID int, pageURL string) (*model.Article, error) {
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	var locked int
	// Compatible with ResolveOwner's KEY SHARE, unlike a lock upgrade to UPDATE.
	if err = q.QueryRow(`SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, ownerID).Scan(&locked); err != nil {
		return nil, err
	}
	feed, err := (&FeedRepository{db: q}).GetOrCreateClipFeed(ownerID)
	if err != nil {
		return nil, err
	}
	a := &model.Article{FeedID: feed.ID, URL: pageURL}
	err = q.QueryRow(`SELECT id,processing_state FROM articles WHERE feed_id=$1 AND url=$2 AND parent_article_id IS NULL ORDER BY id DESC LIMIT 1 FOR UPDATE`, feed.ID, pageURL).Scan(&a.ID, &a.ProcessingState)
	if err == sql.ErrNoRows {
		err = q.QueryRow(`INSERT INTO articles (feed_id,title,url,content,is_clip,processing_state,links_extendable,link_set_suggested) VALUES($1,$2,$2,'',true,'queued',false,false) RETURNING id,processing_state`, feed.ID, pageURL).Scan(&a.ID, &a.ProcessingState)
	} else if err == nil && a.ProcessingState == "failed" {
		_, err = q.Exec(`UPDATE articles SET processing_state='queued',refetch_attempts=0,processing_error='',links_extendable=false,link_set_suggested=false WHERE id=$1`, a.ID)
		a.ProcessingState = "queued"
	}
	if err != nil {
		return nil, err
	}
	if err = commit(); err != nil {
		return nil, err
	}
	return a, nil
}

// Queued captures are distinct from processing PDF OCR jobs and link children.
func (r *ArticleRepository) GetQueuedCapturesAfter(afterID, limit int) ([]model.Article, error) {
	rows, err := r.db.Query(`
		SELECT id, feed_id, title, url, content, published_at,
		       summary_brief, summary_detailed, fetched_at, word_count, reading_minutes,
		       media_url, media_type, media_duration_seconds,
		       links_extendable, parent_article_id, processing_state, COALESCE(processing_error, ''), prerank_score, editor_note, kind
		FROM articles
		WHERE processing_state = 'queued' AND is_clip = true AND parent_article_id IS NULL
		  AND COALESCE(refetch_attempts, 0) < 3 AND id > $2
		ORDER BY id LIMIT $1`, limit, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// CompleteQueuedCapture publishes all fetched metadata with readiness. A stale
// worker must not overwrite a row completed by another capture operation.
func (r *ArticleRepository) CompleteQueuedCapture(id int, content, title string, publishedAt *time.Time, words, minutes int) error {
	_, err := r.db.Exec(`UPDATE articles
		SET content=$2, title=COALESCE(NULLIF($3,''),title), published_at=COALESCE($4,published_at),
		    word_count=$5, reading_minutes=$6, processing_state='ready', processing_error='', refetch_attempts=0, summary_brief=NULL, summary_detailed=NULL
		WHERE id=$1 AND processing_state='queued'`, id, content, title, publishedAt, words, minutes)
	return err
}

func (r *ArticleRepository) FailQueuedCaptureAttempt(id int) error {
	_, err := r.db.Exec(`UPDATE articles
		SET refetch_attempts=COALESCE(refetch_attempts,0)+1,
		    processing_state=CASE WHEN COALESCE(refetch_attempts,0)+1>=3 THEN 'failed' ELSE 'queued' END,
		    processing_error='无法抓取该页面，请重新共享链接重试，或打开原网页使用 RSS Pal 书签保存。'
		WHERE id=$1 AND processing_state='queued'`, id)
	return err
}
