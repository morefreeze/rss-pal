package repository

import (
	"fmt"
	"strings"
	"time"
)

// LinkSetChildInput is the data needed to create one child row.
type LinkSetChildInput struct {
	FeedID          int
	ParentArticleID int
	Title           string
	URL             string
	EditorNote      string
	PrerankScore    float64
	ProcessingState string // 'stub' or 'processing'
	PublishedAt     *time.Time
}

// InsertLinkSetChildren batch-inserts children in a single transaction.
// Returns the number of rows successfully inserted (duplicates are silently
// skipped via ON CONFLICT on uniq_link_set_child_url).
func (r *ArticleRepository) InsertLinkSetChildren(children []LinkSetChildInput) (int, error) {
	if len(children) == 0 {
		return 0, nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var (
		placeholders []string
		args         []interface{}
		idx          = 1
	)
	for _, c := range children {
		placeholders = append(placeholders, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			idx, idx+1, idx+2, idx+3, idx+4, idx+5, idx+6, idx+7, idx+8))
		args = append(args,
			c.FeedID, c.Title, c.URL, c.ParentArticleID, c.EditorNote,
			c.PrerankScore, c.ProcessingState, c.PublishedAt, "")
		idx += 9
	}

	query := fmt.Sprintf(`
		INSERT INTO articles
		    (feed_id, title, url, parent_article_id, editor_note,
		     prerank_score, processing_state, published_at, content)
		VALUES %s
		ON CONFLICT (parent_article_id, url)
		  WHERE parent_article_id IS NOT NULL
		  DO NOTHING
		RETURNING id
	`, strings.Join(placeholders, ", "))

	rows, err := tx.Query(query, args...)
	if err != nil {
		return 0, err
	}
	var inserted int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		inserted++
	}
	rows.Close()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}
