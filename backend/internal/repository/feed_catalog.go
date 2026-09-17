package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lib/pq"
	"time"
)

var ErrCatalogConflict = errors.New("目录已更新或地址重复，请刷新后重试")

type FeedCatalogInput struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Category    string `json:"category"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Revision    int    `json:"revision"`
}
type FeedCatalogEntry struct {
	ID int `json:"id"`
	FeedCatalogInput
	Published     bool       `json:"published"`
	CheckStatus   string     `json:"check_status"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	LastError     string     `json:"last_error"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	SeedKey       *string    `json:"-"`
}
type FeedCatalogRepository struct{ db *sql.DB }

func NewFeedCatalogRepository(db *sql.DB) *FeedCatalogRepository { return &FeedCatalogRepository{db} }

const catalogColumns = `id,title,url,category,description,sort_order,revision,published,check_status,last_checked_at,last_error,created_at,updated_at,seed_key`

func scanCatalog(s interface{ Scan(...any) error }) (FeedCatalogEntry, error) {
	var e FeedCatalogEntry
	err := s.Scan(&e.ID, &e.Title, &e.URL, &e.Category, &e.Description, &e.SortOrder, &e.Revision, &e.Published, &e.CheckStatus, &e.LastCheckedAt, &e.LastError, &e.CreatedAt, &e.UpdatedAt, &e.SeedKey)
	return e, err
}
func (r *FeedCatalogRepository) List(ctx context.Context, public bool) ([]FeedCatalogEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+catalogColumns+` FROM public_feed_catalog WHERE (NOT $1 OR published) ORDER BY sort_order,id`, public)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FeedCatalogEntry, 0)
	for rows.Next() {
		e, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r *FeedCatalogRepository) Get(ctx context.Context, id int) (FeedCatalogEntry, error) {
	return scanCatalog(r.db.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM public_feed_catalog WHERE id=$1`, id))
}
func catalogWrite(e FeedCatalogEntry, err error) (FeedCatalogEntry, error) {
	var p *pq.Error
	if errors.Is(err, sql.ErrNoRows) || errors.As(err, &p) && p.Code == "23505" {
		return e, ErrCatalogConflict
	}
	return e, err
}
func (r *FeedCatalogRepository) Save(ctx context.Context, id, actor int, in FeedCatalogInput) (FeedCatalogEntry, error) {
	if id == 0 {
		return catalogWrite(scanCatalog(r.db.QueryRowContext(ctx, `INSERT INTO public_feed_catalog(title,url,category,description,sort_order,created_by,updated_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($6,0)) RETURNING `+catalogColumns, in.Title, in.URL, in.Category, in.Description, in.SortOrder, actor)))
	}
	return catalogWrite(scanCatalog(r.db.QueryRowContext(ctx, `UPDATE public_feed_catalog SET title=$1,url=$2::varchar,category=$3,description=$4,sort_order=$5,
 published=CASE WHEN url=$2::varchar THEN published ELSE false END,
 check_status=CASE WHEN url=$2::varchar THEN check_status ELSE 'unchecked' END,
 last_checked_at=CASE WHEN url=$2::varchar THEN last_checked_at ELSE NULL END,
 last_error=CASE WHEN url=$2::varchar THEN last_error ELSE '' END,
 revision=revision+1,updated_by=NULLIF($6,0),updated_at=NOW() WHERE id=$7 AND revision=$8 RETURNING `+catalogColumns, in.Title, in.URL, in.Category, in.Description, in.SortOrder, actor, id, in.Revision)))
}
func (r *FeedCatalogRepository) RecordCheck(ctx context.Context, e FeedCatalogEntry, actor int, failure string, publish bool) (FeedCatalogEntry, error) {
	status := "ok"
	if failure != "" {
		status = "failed"
	}
	return catalogWrite(scanCatalog(r.db.QueryRowContext(ctx, `UPDATE public_feed_catalog SET check_status=$1::varchar,last_error=$2,last_checked_at=NOW(),published=CASE WHEN $3 AND $1::varchar='ok' THEN true ELSE published END,revision=revision+1,updated_by=NULLIF($4,0),updated_at=NOW() WHERE id=$5 AND revision=$6 AND url=$7 RETURNING `+catalogColumns, status, failure, publish, actor, e.ID, e.Revision, e.URL)))
}
func (r *FeedCatalogRepository) Unpublish(ctx context.Context, id, actor, revision int) (FeedCatalogEntry, error) {
	return catalogWrite(scanCatalog(r.db.QueryRowContext(ctx, `UPDATE public_feed_catalog SET published=false,revision=revision+1,updated_by=NULLIF($1,0),updated_at=NOW() WHERE id=$2 AND revision=$3 RETURNING `+catalogColumns, actor, id, revision)))
}
