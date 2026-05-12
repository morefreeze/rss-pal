package repository

import (
	"fmt"
	"strings"
	"time"
)

// LinkSetCandidate is one row in link_set_candidates.
type LinkSetCandidate struct {
	ID              int
	ParentArticleID int
	Title           string
	URL             string
	EditorNote      string
	Position        int
}

// ReplaceLinkSetCandidates replaces the cached candidates for a parent
// in one transaction. Old rows are deleted, new ones inserted. Position
// preserves document order from extraction.
func (r *ArticleRepository) ReplaceLinkSetCandidates(parentID int, candidates []LinkSetCandidate) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM link_set_candidates WHERE parent_article_id = $1`, parentID); err != nil {
		return err
	}
	if len(candidates) == 0 {
		return tx.Commit()
	}

	var (
		placeholders []string
		args         []interface{}
		idx          = 1
	)
	for _, c := range candidates {
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)", idx, idx+1, idx+2, idx+3, idx+4))
		args = append(args, parentID, c.URL, c.Title, c.EditorNote, c.Position)
		idx += 5
	}
	query := fmt.Sprintf(
		`INSERT INTO link_set_candidates (parent_article_id, url, title, editor_note, position) VALUES %s
         ON CONFLICT (parent_article_id, url) DO NOTHING`,
		strings.Join(placeholders, ", "),
	)
	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// GetLinkSetCandidates returns the cached candidates for a parent in
// document order (position ASC), with an already_fetched flag derived from
// existing child articles with the same URL.
func (r *ArticleRepository) GetLinkSetCandidates(parentID int) ([]LinkSetCandidate, map[string]bool, error) {
	rows, err := r.db.Query(`
        SELECT id, parent_article_id, url, title, editor_note, position
        FROM link_set_candidates
        WHERE parent_article_id = $1
        ORDER BY position ASC, id ASC
    `, parentID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []LinkSetCandidate
	for rows.Next() {
		var c LinkSetCandidate
		if err := rows.Scan(&c.ID, &c.ParentArticleID, &c.URL, &c.Title, &c.EditorNote, &c.Position); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	// Already-fetched lookup
	childRows, err := r.db.Query(`SELECT url FROM articles WHERE parent_article_id = $1`, parentID)
	if err != nil {
		return out, nil, err
	}
	defer childRows.Close()
	fetched := map[string]bool{}
	for childRows.Next() {
		var u string
		if err := childRows.Scan(&u); err == nil {
			fetched[u] = true
		}
	}
	return out, fetched, nil
}

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
