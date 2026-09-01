package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

const (
	MaxExploreSnapshotSources     = 12
	MaxExploreSnapshotTopicRunes  = 100
	MaxExploreSnapshotReasonRunes = 500
	MaxExploreSnapshotErrorRunes  = 1000
)

var (
	ErrInvalidExploreSnapshot = errors.New("invalid explore snapshot")
	ErrExploreSnapshotFence   = errors.New("explore snapshot generation is not owned by this token")
)

// ExploreColdStartSlotAt is a per-user sentinel slot. A real scheduled slot is
// always later, so normal snapshots supersede this persisted authorization.
var ExploreColdStartSlotAt = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

const exploreLatestDoneBatchSQL = `
	SELECT id, user_id, slot_at, status, source_count, error_message,
	       generation_token, started_at, created_at, completed_at
	FROM explore_batches
	WHERE user_id=$1 AND status='done'
	ORDER BY slot_at DESC, id DESC
	LIMIT 1
	FOR SHARE
`

type ExploreSnapshotSourceInput struct {
	SourceID int
	Score    float64
	Topic    string
	Reason   string
}

// ExploreSnapshotGenerationToken is an opaque fence credential. It can be
// passed back to Publish or Fail, but formatting it never reveals its value.
type ExploreSnapshotGenerationToken struct {
	value string
}

func (ExploreSnapshotGenerationToken) String() string {
	return "[REDACTED]"
}

func (ExploreSnapshotGenerationToken) GoString() string {
	return "repository.ExploreSnapshotGenerationToken{[REDACTED]}"
}

func (token ExploreSnapshotGenerationToken) IsZero() bool {
	return token.value == ""
}

type ExploreSnapshotClaim struct {
	Batch           model.ExploreBatch             `json:"batch"`
	GenerationToken ExploreSnapshotGenerationToken `json:"-"`
}

type ExploreSnapshotRepository struct {
	db    Querier
	rawDB *sql.DB
}

func NewExploreSnapshotRepository(db *sql.DB) *ExploreSnapshotRepository {
	return &ExploreSnapshotRepository{db: db, rawDB: db}
}

func (r *ExploreSnapshotRepository) WithQuerier(db Querier) *ExploreSnapshotRepository {
	return &ExploreSnapshotRepository{db: db, rawDB: r.rawDB}
}

func (r *ExploreSnapshotRepository) WithCtx(c ctxkey.CtxGetter) *ExploreSnapshotRepository {
	if value, ok := c.Get(ctxkey.Tx); ok {
		if db, ok := value.(Querier); ok {
			return r.WithQuerier(db)
		}
	}
	return r
}

// Claim creates the unique user/slot batch or takes over a failed or stale
// generation. A successful takeover always rotates the opaque fence token.
func (r *ExploreSnapshotRepository) Claim(userID int, slotAt, now time.Time, staleAfter time.Duration) (*ExploreSnapshotClaim, bool, error) {
	if userID <= 0 || slotAt.IsZero() || now.IsZero() || staleAfter <= 0 {
		return nil, false, fmt.Errorf("%w: invalid claim", ErrInvalidExploreSnapshot)
	}
	token, err := newExploreGenerationToken()
	if err != nil {
		return nil, false, err
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, false, err
	}
	defer rollback()

	batch, err := scanExploreBatch(tx.QueryRow(`
		INSERT INTO explore_batches (user_id, slot_at, status, generation_token, started_at)
		VALUES ($1, $2, 'pending', $3, $4)
		ON CONFLICT (user_id, slot_at) DO NOTHING
		RETURNING id, user_id, slot_at, status, source_count, error_message,
		          generation_token, started_at, created_at, completed_at
	`, userID, slotAt, token.value, now))
	if err == nil {
		if err := commit(); err != nil {
			return nil, false, err
		}
		return &ExploreSnapshotClaim{Batch: *batch, GenerationToken: token}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	batch, err = scanExploreBatch(tx.QueryRow(`
		SELECT id, user_id, slot_at, status, source_count, error_message,
		       generation_token, started_at, created_at, completed_at
		FROM explore_batches
		WHERE user_id=$1 AND slot_at=$2
		FOR UPDATE
	`, userID, slotAt))
	if err != nil {
		return nil, false, err
	}
	if batch.Status == model.ExploreBatchDone {
		if err := commit(); err != nil {
			return nil, false, err
		}
		return &ExploreSnapshotClaim{Batch: *batch}, false, nil
	}
	stale := batch.StartedAt == nil || !batch.StartedAt.After(now.Add(-staleAfter))
	if batch.Status != model.ExploreBatchFailed && !stale {
		if err := commit(); err != nil {
			return nil, false, err
		}
		return &ExploreSnapshotClaim{Batch: *batch}, false, nil
	}
	if _, err := tx.Exec(`DELETE FROM explore_batch_sources WHERE batch_id=$1 AND user_id=$2`, batch.ID, userID); err != nil {
		return nil, false, err
	}
	batch, err = scanExploreBatch(tx.QueryRow(`
		UPDATE explore_batches
		SET status='pending', source_count=0, error_message=NULL,
		    generation_token=$3, started_at=$4, completed_at=NULL
		WHERE id=$1 AND user_id=$2 AND status<>'done'
		RETURNING id, user_id, slot_at, status, source_count, error_message,
		          generation_token, started_at, created_at, completed_at
	`, batch.ID, userID, token.value, now))
	if err != nil {
		return nil, false, err
	}
	if err := commit(); err != nil {
		return nil, false, err
	}
	return &ExploreSnapshotClaim{Batch: *batch, GenerationToken: token}, true, nil
}

// Publish validates the complete result before opening its short transaction,
// locks all current valid canonical sources, rewrites rank, and atomically
// marks the fenced batch done.
func (r *ExploreSnapshotRepository) Publish(batchID int, token ExploreSnapshotGenerationToken, values []ExploreSnapshotSourceInput) (*model.ExploreBatch, error) {
	if batchID <= 0 || token.IsZero() {
		return nil, ErrExploreSnapshotFence
	}
	if err := r.verifyExploreSnapshotFence(batchID, token); err != nil {
		return nil, err
	}
	if err := validateExploreSnapshotSources(values); err != nil {
		return nil, err
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	batch, err := scanExploreBatch(tx.QueryRow(`
		SELECT id, user_id, slot_at, status, source_count, error_message,
		       generation_token, started_at, created_at, completed_at
		FROM explore_batches
		WHERE id=$1 AND generation_token=$2 AND status='pending'
		FOR UPDATE
	`, batchID, token.value))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExploreSnapshotFence
	}
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(values))
	for index := range values {
		ids[index] = values[index].SourceID
	}
	if len(ids) > 0 {
		rows, err := tx.Query(`
			SELECT id FROM recommended_feeds
			WHERE id=ANY($1) AND validation_status='valid'
			  AND is_broken=false AND merged_into_source_id IS NULL
			ORDER BY id FOR SHARE
		`, pq.Array(ids))
		if err != nil {
			return nil, err
		}
		validCount := 0
		for rows.Next() {
			validCount++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if validCount != len(ids) {
			return nil, fmt.Errorf("%w: snapshot contains a missing, invalid, broken, or merged source", ErrInvalidExploreSnapshot)
		}
	}
	for index, value := range values {
		if _, err := tx.Exec(`
			INSERT INTO explore_batch_sources (user_id,batch_id,source_id,rank,score,topic,reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
		`, batch.UserID, batch.ID, value.SourceID, index+1, value.Score, nullableText(value.Topic), nullableText(value.Reason)); err != nil {
			return nil, err
		}
	}
	batch, err = scanExploreBatch(tx.QueryRow(`
		UPDATE explore_batches
		SET status='done', source_count=$3, error_message=NULL,
		    generation_token=NULL, completed_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND generation_token=$2 AND status='pending'
		RETURNING id, user_id, slot_at, status, source_count, error_message,
		          generation_token, started_at, created_at, completed_at
	`, batch.ID, token.value, len(values)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExploreSnapshotFence
	}
	if err != nil {
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return batch, nil
}

func (r *ExploreSnapshotRepository) verifyExploreSnapshotFence(batchID int, token ExploreSnapshotGenerationToken) error {
	var exists int
	err := r.db.QueryRow(`
		SELECT 1 FROM explore_batches
		WHERE id=$1 AND generation_token=$2 AND status='pending'
	`, batchID, token.value).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExploreSnapshotFence
	}
	return err
}

func (r *ExploreSnapshotRepository) Fail(batchID int, token ExploreSnapshotGenerationToken, cause error) error {
	if batchID <= 0 || token.IsZero() {
		return ErrExploreSnapshotFence
	}
	message := "snapshot generation failed"
	if cause != nil {
		message = cause.Error()
	}
	message = truncateRunes(message, MaxExploreSnapshotErrorRunes)
	result, err := r.db.Exec(`
		UPDATE explore_batches
		SET status='failed', error_message=$3, generation_token=NULL, completed_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND generation_token=$2 AND status='pending'
	`, batchID, token.value, message)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrExploreSnapshotFence
	}
	return nil
}

func (r *ExploreSnapshotRepository) LatestDone(userID int) (*model.ExploreBatch, []model.ExploreBatchSource, error) {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, nil, err
	}
	defer rollback()
	batch, err := scanExploreBatch(tx.QueryRow(exploreLatestDoneBatchSQL, userID))
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(`
		SELECT id,user_id,batch_id,source_id,rank,score,topic,reason
		FROM explore_batch_sources
		WHERE batch_id=$1 AND user_id=$2
		ORDER BY rank ASC, id ASC
	`, batch.ID, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	values := make([]model.ExploreBatchSource, 0, batch.SourceCount)
	for rows.Next() {
		var value model.ExploreBatchSource
		if err := rows.Scan(&value.ID, &value.UserID, &value.BatchID, &value.SourceID, &value.Rank, &value.Score, &value.Topic, &value.Reason); err != nil {
			return nil, nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := commit(); err != nil {
		return nil, nil, err
	}
	return batch, values, nil
}

func (r *ExploreSnapshotRepository) Cleanup(now time.Time) (int64, int64, error) {
	if now.IsZero() {
		return 0, 0, fmt.Errorf("%w: cleanup time is required", ErrInvalidExploreSnapshot)
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, 0, err
	}
	defer rollback()
	eventResult, err := tx.Exec(`DELETE FROM explore_article_events WHERE occurred_at < $1`, now.Add(-180*24*time.Hour))
	if err != nil {
		return 0, 0, err
	}
	batchResult, err := tx.Exec(`DELETE FROM explore_batches WHERE created_at < $1`, now.Add(-30*24*time.Hour))
	if err != nil {
		return 0, 0, err
	}
	events, err := eventResult.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	batches, err := batchResult.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if err := commit(); err != nil {
		return 0, 0, err
	}
	return batches, events, nil
}

func scanExploreBatch(scanner interface{ Scan(...interface{}) error }) (*model.ExploreBatch, error) {
	var batch model.ExploreBatch
	var generationToken sql.NullString
	if err := scanner.Scan(
		&batch.ID, &batch.UserID, &batch.SlotAt, &batch.Status, &batch.SourceCount,
		&batch.ErrorMessage, &generationToken, &batch.StartedAt, &batch.CreatedAt, &batch.CompletedAt,
	); err != nil {
		return nil, err
	}
	return &batch, nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func validateExploreSnapshotSources(values []ExploreSnapshotSourceInput) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: at least one source is required", ErrInvalidExploreSnapshot)
	}
	if len(values) > MaxExploreSnapshotSources {
		return fmt.Errorf("%w: at most %d sources", ErrInvalidExploreSnapshot, MaxExploreSnapshotSources)
	}
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value.SourceID <= 0 {
			return fmt.Errorf("%w: source id must be positive", ErrInvalidExploreSnapshot)
		}
		if _, exists := seen[value.SourceID]; exists {
			return fmt.Errorf("%w: duplicate source %d", ErrInvalidExploreSnapshot, value.SourceID)
		}
		seen[value.SourceID] = struct{}{}
		if math.IsNaN(value.Score) || math.IsInf(value.Score, 0) {
			return fmt.Errorf("%w: source %d score must be finite", ErrInvalidExploreSnapshot, value.SourceID)
		}
		if !utf8.ValidString(value.Topic) || strings.ContainsRune(value.Topic, '\x00') {
			return fmt.Errorf("%w: source %d topic is not valid text", ErrInvalidExploreSnapshot, value.SourceID)
		}
		if !utf8.ValidString(value.Reason) || strings.ContainsRune(value.Reason, '\x00') {
			return fmt.Errorf("%w: source %d reason is not valid text", ErrInvalidExploreSnapshot, value.SourceID)
		}
		if utf8.RuneCountInString(value.Topic) > MaxExploreSnapshotTopicRunes {
			return fmt.Errorf("%w: source %d topic is too long", ErrInvalidExploreSnapshot, value.SourceID)
		}
		if utf8.RuneCountInString(value.Reason) > MaxExploreSnapshotReasonRunes {
			return fmt.Errorf("%w: source %d reason is too long", ErrInvalidExploreSnapshot, value.SourceID)
		}
	}
	return nil
}

func newExploreGenerationToken() (ExploreSnapshotGenerationToken, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return ExploreSnapshotGenerationToken{}, fmt.Errorf("generate explore snapshot token: %w", err)
	}
	return ExploreSnapshotGenerationToken{value: hex.EncodeToString(value)}, nil
}
