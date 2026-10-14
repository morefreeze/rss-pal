// backfill_clip_dates repairs verified dates on explicitly scoped web clips.
// Defaults to a read-only preview; --apply enables guarded, date-only writes.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

type repairScope struct {
	Owner        int
	Since, Until time.Time
}
type candidate struct {
	ID        int
	URL       string
	FetchedAt time.Time
}
type report struct {
	ID          int        `json:"id"`
	URL         string     `json:"url"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Shares      int64      `json:"shares_updated,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func main() {
	owner := flag.Int("owner", 0, "required owner ID")
	sinceRaw := flag.String("since", "", "required inclusive capture cutoff (RFC3339)")
	untilRaw := flag.String("until", time.Now().UTC().Format(time.RFC3339), "exclusive capture end (RFC3339)")
	apply := flag.Bool("apply", false, "apply verified dates (default: preview only)")
	flag.Parse()
	since, err := time.Parse(time.RFC3339, *sinceRaw)
	if err != nil || *owner <= 0 {
		log.Fatal("positive --owner and RFC3339 --since are required")
	}
	until, err := time.Parse(time.RFC3339, *untilRaw)
	if err != nil || !until.After(since) {
		log.Fatal("--until must be later than --since")
	}
	scope := repairScope{*owner, since.UTC(), until.UTC()}
	cfg := config.Load()
	db, err := repository.NewBypassDB(&cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	rows, err := selectCandidates(ctx, db, scope)
	if err != nil {
		log.Fatal(err)
	}
	fetcher := rss.NewContentFetcher()
	out := json.NewEncoder(os.Stdout)
	failures := 0
	for _, row := range rows {
		r := report{ID: row.ID, URL: row.URL}
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		date, err := fetcher.FetchPublicationDate(fetchCtx, row.URL)
		cancel()
		switch {
		case err != nil:
			r.Status = "fetch_failed"
			r.Error = err.Error()
		case date == nil:
			r.Status = "no_verified_date"
		case date.After(row.FetchedAt.Add(24 * time.Hour)):
			r.Status = "date_after_capture"
		default:
			r.PublishedAt = date
			r.Status = "preview"
			if *apply {
				var changed bool
				changed, r.Shares, err = applyDate(ctx, db, scope, row, *date)
				if err != nil {
					r.Status = "write_failed"
					r.Error = err.Error()
				} else if changed {
					r.Status = "updated"
				} else {
					r.Status = "skipped_changed_record"
				}
			}
		}
		if err := out.Encode(r); err != nil {
			log.Fatal(err)
		}
		if r.Status == "fetch_failed" || r.Status == "write_failed" {
			failures++
		}
	}
	if failures > 0 {
		log.Fatalf("%d of %d articles failed; see per-article report", failures, len(rows))
	}
}

func selectCandidates(ctx context.Context, db *sql.DB, scope repairScope) ([]candidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT a.id,a.url,a.fetched_at FROM articles a JOIN feeds f ON f.id=a.feed_id
	WHERE f.owner_id=$1 AND f.url LIKE 'bookmarklet://user/%' AND a.is_clip AND a.published_at IS NULL
	AND a.fetched_at >= $2 AND a.fetched_at < $3 ORDER BY a.id`, scope.Owner, scope.Since.UTC(), scope.Until.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []candidate
	for rows.Next() {
		var row candidate
		if err := rows.Scan(&row.ID, &row.URL, &row.FetchedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func applyDate(ctx context.Context, db *sql.DB, scope repairScope, row candidate, date time.Time) (bool, int64, error) {
	if date.IsZero() || date.After(row.FetchedAt.Add(24*time.Hour)) {
		return false, 0, fmt.Errorf("invalid publication date")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE articles a SET published_at=$1 FROM feeds f WHERE f.id=a.feed_id
	AND a.id=$2 AND a.url=$3 AND f.owner_id=$4 AND f.url LIKE 'bookmarklet://user/%' AND a.is_clip AND a.published_at IS NULL
	AND a.fetched_at >= $5 AND a.fetched_at < $6`, date.UTC(), row.ID, row.URL, scope.Owner, scope.Since.UTC(), scope.Until.UTC())
	if err != nil {
		return false, 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	if count == 0 {
		return false, 0, nil
	}
	result, err = tx.ExecContext(ctx, `UPDATE article_shares SET snapshot=jsonb_set(snapshot,'{published_at}',to_jsonb($1::text),true)
	WHERE article_id=$2 AND created_by=$3 AND snapshot->>'published_at' IS NULL
	AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, date.UTC().Format(time.RFC3339Nano), row.ID, scope.Owner)
	if err != nil {
		return false, 0, err
	}
	shares, err := result.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	return true, shares, nil
}
