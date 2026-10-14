package repository

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

const CaptureNoteInputLimit = 2000
const CaptureNoteTotalLimit = 10000

var ErrCaptureNoteTooLong = errors.New("capture note is too long")

type CaptureNoteRepository struct{ db Querier }

func NewCaptureNoteRepository(db *sql.DB) *CaptureNoteRepository {
	return &CaptureNoteRepository{db: db}
}
func (r *CaptureNoteRepository) WithCtx(c ctxkey.CtxGetter) *CaptureNoteRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &CaptureNoteRepository{db: q}
		}
	}
	return r
}

func (r *CaptureNoteRepository) authorize(userID, articleID int) error {
	var id int
	return r.db.QueryRow(`SELECT a.id FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1 AND f.owner_id=$2`, articleID, userID).Scan(&id)
}

func (r *CaptureNoteRepository) Get(userID, articleID int) (string, error) {
	if err := r.authorize(userID, articleID); err != nil {
		return "", err
	}
	var note string
	err := r.db.QueryRow(`SELECT capture_note FROM article_capture_notes WHERE user_id=$1 AND article_id=$2`, userID, articleID).Scan(&note)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return note, err
}

// Append is atomic even when duplicate capture requests race. A blank input
// leaves the existing note untouched; an identical final entry is idempotent.
func (r *CaptureNoteRepository) Append(userID, articleID int, note string) (string, error) {
	if utf8.RuneCountInString(note) > CaptureNoteInputLimit {
		return "", ErrCaptureNoteTooLong
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return "", nil
	}
	if err := r.authorize(userID, articleID); err != nil {
		return "", err
	}
	// The upsert's row lock protects both the length check and append. No update
	// occurs when the total would exceed the limit, so callers can roll back the
	// enclosing request without accepting truncated user text.
	const merged = `CASE WHEN article_capture_notes.capture_note='' THEN EXCLUDED.capture_note
 WHEN article_capture_notes.capture_note=EXCLUDED.capture_note
 OR right(article_capture_notes.capture_note,char_length(EXCLUDED.capture_note)+2)=E'\n\n'||EXCLUDED.capture_note
 THEN article_capture_notes.capture_note
 ELSE article_capture_notes.capture_note||E'\n\n'||EXCLUDED.capture_note END`
	var saved string
	err := r.db.QueryRow(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)
 ON CONFLICT (user_id,article_id) DO UPDATE SET capture_note=`+merged+`,updated_at=NOW()
 WHERE char_length(`+merged+`)<=10000 RETURNING capture_note`, userID, articleID, note).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCaptureNoteTooLong
	}
	return saved, err
}

// Replace deliberately accepts empty text so the reader can clear a note.
func (r *CaptureNoteRepository) Replace(userID, articleID int, note string) (string, error) {
	if utf8.RuneCountInString(note) > CaptureNoteTotalLimit {
		return "", ErrCaptureNoteTooLong
	}
	if err := r.authorize(userID, articleID); err != nil {
		return "", err
	}
	_, err := r.db.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)
 ON CONFLICT (user_id,article_id) DO UPDATE SET capture_note=EXCLUDED.capture_note,updated_at=NOW()`, userID, articleID, note)
	return note, err
}
