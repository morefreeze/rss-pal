package repository

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lib/pq"
)

var ErrReadUndoNotFound = errors.New("read undo expired or already used")

type ReadMutationResult struct {
	ArticleIDs []int  `json:"article_ids"`
	UndoToken  string `json:"undo_token"`
}

// executeReadMutation shares the atomic write/snapshot step between all-read
// and chronological range actions. ctes defines scoped(id,is_read,previous)
// and optionally anchor; boundary/anchorCheck are internal SQL, never input.
func (r *ProgressRepository) executeReadMutation(ctes, boundary, anchorCheck string, args []any) (ReadMutationResult, error) {
	result := ReadMutationResult{ArticleIDs: []int{}}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return result, err
	}
	args = append(args, hex.EncodeToString(secret))
	tokenArg := fmt.Sprintf("$%d", len(args))
	// ON CONFLICT can wait for a newer committed row than scoped's snapshot.
	// Compare the entire previous row before overwriting so undo never stores
	// stale state from a concurrent insert or partial reading-progress update.
	query := `WITH ` + ctes + `, changed AS (
 INSERT INTO reading_progress(user_id,article_id,scroll_position,last_read_at,is_completed)
 SELECT $1,s.id,1.0,NOW(),true FROM scoped s
 WHERE NOT s.is_read AND (` + anchorCheck + `) AND (` + boundary + `)
 ON CONFLICT(article_id,user_id) DO UPDATE SET
 is_completed=true,scroll_position=1.0,last_read_at=NOW()
 WHERE reading_progress.is_completed IS DISTINCT FROM true
 AND to_jsonb(reading_progress) = (SELECT previous FROM scoped WHERE id=EXCLUDED.article_id)
 RETURNING article_id
 ), expired AS (
 DELETE FROM reading_read_undo WHERE user_id=$1 AND expires_at < NOW()
 ), undo AS (
 INSERT INTO reading_read_undo(token,user_id,snapshot,marked_at,expires_at)
 SELECT ` + tokenArg + `,$1,jsonb_agg(jsonb_build_object('article_id',c.article_id,'previous',s.previous)),NOW(),NOW()+INTERVAL '10 minutes'
 FROM changed c JOIN scoped s ON s.id=c.article_id HAVING count(*)>0
 RETURNING token
 ) SELECT (` + anchorCheck + `),COALESCE((SELECT array_agg(article_id) FROM changed),'{}'::int[]),COALESCE((SELECT token FROM undo),'')`
	var exists bool
	var ids pq.Int64Array
	if err := r.db.QueryRow(query, args...).Scan(&exists, &ids, &result.UndoToken); err != nil {
		return result, err
	}
	if !exists {
		return result, ErrReadAnchorNotFound
	}
	for _, id := range ids {
		result.ArticleIDs = append(result.ArticleIDs, int(id))
	}
	return result, nil
}

// UndoMarkRead consumes an owner-bound token and restores only rows that still
// carry this operation's timestamp. Later reading and repeated undo are safe.
func (r *ProgressRepository) UndoMarkRead(userID int, token string) ([]int, error) {
	query := `WITH claimed AS (
 DELETE FROM reading_read_undo WHERE token=$2 AND user_id=$1 AND expires_at>=NOW()
 RETURNING snapshot,marked_at
 ), states AS (
 SELECT item,marked_at FROM claimed,jsonb_array_elements(snapshot) item
 ), restored AS (
 UPDATE reading_progress p SET
 scroll_position=(s.item->'previous'->>'scroll_position')::double precision,
 last_read_at=(s.item->'previous'->>'last_read_at')::timestamp,
 is_completed=(s.item->'previous'->>'is_completed')::boolean
 FROM states s WHERE p.user_id=$1 AND p.article_id=(s.item->>'article_id')::int
 AND s.item->'previous' <> 'null'::jsonb
 AND p.last_read_at=s.marked_at AND p.is_completed AND p.scroll_position=1.0
 RETURNING p.article_id
 ), removed AS (
 DELETE FROM reading_progress p USING states s
 WHERE p.user_id=$1 AND p.article_id=(s.item->>'article_id')::int
 AND s.item->'previous'='null'::jsonb
 AND p.last_read_at=s.marked_at AND p.is_completed AND p.scroll_position=1.0
 RETURNING p.article_id
 ) SELECT EXISTS(SELECT 1 FROM claimed),COALESCE((SELECT array_agg(article_id) FROM (SELECT article_id FROM restored UNION ALL SELECT article_id FROM removed) x),'{}'::int[])`
	var exists bool
	var raw pq.Int64Array
	if err := r.db.QueryRow(query, userID, token).Scan(&exists, &raw); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrReadUndoNotFound
	}
	ids := make([]int, len(raw))
	for i, id := range raw {
		ids[i] = int(id)
	}
	return ids, nil
}
