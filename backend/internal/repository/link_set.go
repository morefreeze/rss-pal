package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq"
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
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return err
	}
	defer rollback()

	if _, err := tx.Exec(`DELETE FROM link_set_candidates WHERE parent_article_id = $1`, parentID); err != nil {
		return err
	}
	if len(candidates) == 0 {
		return commit()
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
	return commit()
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
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()
	inserted, err := insertLinkSetChildren(tx, children)
	if err != nil {
		return 0, err
	}
	if err := commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// EnableAndInsertLinkSetChildren promotes a parent and inserts its selected
// children in one transaction. When r is already bound to the request
// transaction, the outer RLS middleware owns the final commit/rollback.
func (r *ArticleRepository) EnableAndInsertLinkSetChildren(parentID int, children []LinkSetChildInput) (int, error) {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()

	result, err := tx.Exec(`UPDATE articles SET links_extendable = true WHERE id = $1`, parentID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected != 1 {
		return 0, fmt.Errorf("parent article %d not found", parentID)
	}
	inserted := 0
	seen := make(map[string]bool, len(children))
	for _, child := range children {
		if child.ParentArticleID != parentID {
			return 0, fmt.Errorf("child does not belong to parent article %d", parentID)
		}
		canonical, err := normalizeCaptureDraftURL(child.URL)
		if err != nil {
			return 0, err
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		aliases := captureDraftURLAliases(canonical)
		// A root origin may have been stored with or without its slash. Resolve
		// the existing child before inserting so both forms share the same note.
		var childID int
		err = tx.QueryRow(`SELECT id FROM articles WHERE parent_article_id=$1 AND url=ANY($2) ORDER BY id LIMIT 1`, parentID, pq.Array(aliases)).Scan(&childID)
		if errors.Is(err, sql.ErrNoRows) {
			n, insertErr := insertLinkSetChildren(tx, []LinkSetChildInput{child})
			if insertErr != nil {
				return 0, insertErr
			}
			inserted += n
			err = tx.QueryRow(`SELECT id FROM articles WHERE parent_article_id=$1 AND url=ANY($2) ORDER BY id LIMIT 1`, parentID, pq.Array(aliases)).Scan(&childID)
		}
		if err != nil {
			return 0, err
		}
		var userID int
		var note string
		// Read the latest server draft under the parent lock held by UPDATE.
		// Only the parent's owner can transfer a draft to their own child.
		err = tx.QueryRow(`DELETE FROM article_capture_drafts d WHERE d.article_id=$1 AND d.url=$2
 AND d.user_id=(SELECT f.owner_id FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1)
 AND EXISTS(SELECT 1 FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$3 AND f.owner_id=d.user_id)
 RETURNING d.user_id,d.capture_note`, parentID, canonical, childID).Scan(&userID, &note)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if _, err := (&CaptureNoteRepository{db: tx}).appendStored(userID, childID, note); err != nil {
			return 0, err
		}
	}
	if err := commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func insertLinkSetChildren(q Querier, children []LinkSetChildInput) (int, error) {
	if len(children) == 0 {
		return 0, nil
	}

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

	rows, err := q.Query(query, args...)
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
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return inserted, nil
}
