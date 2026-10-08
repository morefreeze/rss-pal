package explore

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bytedance/rss-pal/internal/rss"
)

type ContentRepairOptions struct {
	Apply    bool
	Backup   func(ContentRepairBackup) error
	Progress func(ContentRepairStats)
}
type ContentRepairStats struct{ Scanned, Legacy, Changed, Articles int }
type ContentRepairBackup struct {
	Explore    json.RawMessage              `json:"explore"`
	Articles   []ContentRepairArticleBackup `json:"articles"`
	NewContent sql.NullString               `json:"new_content"`
}

type ContentRepairArticleBackup struct {
	Before     json.RawMessage `json:"before"`
	NewContent string          `json:"new_content"`
}

// RepairContent scans a fixed high-water mark, in batches, so fresh fetches can
// continue. It also reconciles exact imported HTML copies after a concurrent
// fetch has already normalized their cache. Different bodies are never replaced.
func RepairContent(ctx context.Context, db *sql.DB, opts ContentRepairOptions) (ContentRepairStats, error) {
	var stats ContentRepairStats
	if opts.Apply && opts.Backup == nil {
		return stats, errors.New("apply requires a durable backup callback")
	}
	var maxID int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM explore_articles`).Scan(&maxID); err != nil {
		return stats, err
	}
	cursor := 0
	for cursor < maxID {
		rows, err := db.QueryContext(ctx, `SELECT id FROM explore_articles WHERE id>$1 AND id<=$2 ORDER BY id LIMIT 100`, cursor, maxID)
		if err != nil {
			return stats, err
		}
		var ids []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return stats, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return stats, err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			one, err := repairContentRow(ctx, db, id, opts)
			if err != nil {
				return stats, fmt.Errorf("explore article %d: %w", id, err)
			}
			stats.Scanned++
			stats.Legacy += one.Legacy
			stats.Changed += one.Changed
			stats.Articles += one.Articles
			cursor = id
		}
		if opts.Progress != nil {
			opts.Progress(stats)
		}
	}
	return stats, nil
}

func repairContentRow(ctx context.Context, db *sql.DB, id int, opts ContentRepairOptions) (ContentRepairStats, error) {
	var stats ContentRepairStats
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return stats, err
	}
	lock := ""
	if opts.Apply {
		lock = " FOR UPDATE OF e"
	}
	var raw, legacyHash sql.NullString
	var version int
	var articleURL, feedURL string
	var backup ContentRepairBackup
	err = tx.QueryRowContext(ctx, `SELECT e.content,e.content_version,e.url,s.url,to_jsonb(e),e.legacy_content_hash
 FROM explore_articles e JOIN recommended_feeds s ON s.id=e.source_id WHERE e.id=$1`+lock, id).Scan(&raw, &version, &articleURL, &feedURL, &backup.Explore, &legacyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	if version > rss.FeedContentVersion {
		return stats, nil
	}
	normalized := raw
	if version < rss.FeedContentVersion {
		stats.Legacy = 1
		if raw.Valid {
			normalized.String = rss.NormalizeFeedContent(raw.String, articleURL)
		}
		if normalized != raw {
			stats.Changed = 1
		}
	}
	backup.NewContent = normalized
	// Scope to the same source, URL and ordinary article shape. Full rows are
	// captured under row locks before any updates for recoverable backups.
	lock = ""
	if opts.Apply {
		lock = " FOR UPDATE OF a"
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.content,to_jsonb(a) FROM articles a
 JOIN feeds f ON f.id=a.feed_id
 WHERE f.url=$1 AND a.url=$2 AND NOT a.is_clip AND a.parent_article_id IS NULL
 AND a.content IS NOT NULL AND a.content IS DISTINCT FROM $3 ORDER BY a.id`+lock, feedURL, articleURL, normalized)
	if err != nil {
		return stats, err
	}
	type imported struct {
		id        int
		old       string
		converted string
	}
	var copies []imported
	for rows.Next() {
		var a imported
		var snapshot json.RawMessage
		if err := rows.Scan(&a.id, &a.old, &snapshot); err != nil {
			rows.Close()
			return stats, err
		}
		exactLegacy := version == 0 && raw.Valid && a.old == raw.String
		retainedLegacy := version == 1 && legacyHash.Valid && fmt.Sprintf("%x", md5.Sum([]byte(a.old))) == legacyHash.String
		if exactLegacy || retainedLegacy {
			a.converted = rss.NormalizeFeedContent(a.old, articleURL)
			if a.converted != a.old {
				copies = append(copies, a)
				backup.Articles = append(backup.Articles, ContentRepairArticleBackup{Before: snapshot, NewContent: a.converted})
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return stats, err
	}
	stats.Articles = len(copies)
	if !opts.Apply || (stats.Legacy == 0 && len(copies) == 0) {
		return stats, nil
	}
	// The caller must flush AND fsync before returning. Backup failure aborts
	// this transaction; earlier committed rows are safe to skip on a rerun.
	if err := opts.Backup(backup); err != nil {
		return stats, err
	}
	if stats.Legacy != 0 {
		result, err := tx.ExecContext(ctx, `UPDATE explore_articles SET legacy_content_hash=md5(content),content=$2,content_version=$3
   WHERE id=$1 AND content_version=$4 AND content IS NOT DISTINCT FROM $5`, id, normalized, rss.FeedContentVersion, version, raw)
		if err != nil {
			return stats, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return stats, fmt.Errorf("cache changed: affected=%d err=%v", n, err)
		}
	}
	for _, a := range copies {
		words, minutes := rss.ComputeMetrics(a.converted)
		result, err := tx.ExecContext(ctx, `UPDATE articles SET content=$2,word_count=$3,reading_minutes=$4,
   summary_brief=NULL,summary_detailed=NULL,image_dimensions=NULL
   WHERE id=$1 AND content=$5`, a.id, a.converted, words, minutes, a.old)
		if err != nil {
			return stats, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return stats, fmt.Errorf("import changed: affected=%d err=%v", n, err)
		}
	}
	return stats, tx.Commit()
}
