package repository

import (
	"errors"
	"fmt"
	"strings"
)

var ErrReadAnchorNotFound = errors.New("article is outside the current list")

type ReadRange struct {
	Filter    ArticleFilter
	AnchorID  int
	Direction string // single, above, below; inclusive of anchor
	Sort      SortMode
	Dir       SortDir
	ClipOnly  bool
	Source    string // clip source: feed:<id> or host:<name>
}

// MarkReadRange applies the boundary in SQL, independent of client pagination.
// The anchor is resolved before excluding read rows, so an already-read anchor
// remains usable in an unread-only list. All writes use the caller's RLS tx.
func (r *ProgressRepository) MarkReadRange(q ReadRange) ([]int, error) {
	result, err := r.MarkReadRangeWithUndo(q)
	return result.ArticleIDs, err
}

func (r *ProgressRepository) MarkReadRangeWithUndo(q ReadRange) (ReadMutationResult, error) {
	if q.AnchorID <= 0 || (q.Direction != "single" && q.Direction != "above" && q.Direction != "below") {
		return ReadMutationResult{}, fmt.Errorf("invalid read range")
	}
	filter := q.Filter
	filter.UnreadOnly = false
	joins, where, args, next := buildArticleFilterSQL(filter, "articles", 1)
	where = append(where, "feeds.owner_id = $1")
	if q.ClipOnly {
		where = append(where, "feeds.feed_type = 'clip'")
	}
	if q.Source != "" {
		kind, value, ok := strings.Cut(q.Source, ":")
		if !q.ClipOnly || !ok || value == "" {
			return ReadMutationResult{}, fmt.Errorf("invalid source")
		}
		switch kind {
		case "feed":
			where = append(where, fmt.Sprintf("articles.feed_id::text = $%d", next))
		case "host":
			where = append(where, fmt.Sprintf(`lower(regexp_replace(articles.url, '^https?://(?:www\.)?([^/]+).*$', '\1')) = lower($%d)`, next))
		default:
			return ReadMutationResult{}, fmt.Errorf("invalid source")
		}
		args = append(args, value)
		next++
	}
	keys := articleSortExpressions("articles", q.Sort)
	// Captured sort has one timestamp; duplicate it to share the tuple shape.
	if len(keys) == 1 {
		keys = append(keys, keys[0])
	}
	anchorParam := fmt.Sprintf("$%d", next)
	args = append(args, q.AnchorID)
	boundary := "s.id = " + anchorParam
	if q.Direction != "single" {
		op := "<="
		if (q.Direction == "above") == (q.Dir == SortDesc) {
			op = ">="
		}
		boundary = "(s.key1, s.key2, s.id) " + op + " (SELECT key1, key2, id FROM anchor)"
	}
	ctes := `scoped AS MATERIALIZED (
 SELECT articles.id, to_jsonb(rp) AS previous, COALESCE(rp.is_completed,false) AS is_read, ` + keys[0] + ` AS key1, ` + keys[1] + ` AS key2
 FROM articles JOIN feeds ON feeds.id=articles.feed_id ` + joins + `
 WHERE ` + strings.Join(where, " AND ") + `
 ), anchor AS (SELECT * FROM scoped WHERE id=` + anchorParam + `)`
	return r.executeReadMutation(ctes, boundary, "EXISTS(SELECT 1 FROM anchor)", args)
}
