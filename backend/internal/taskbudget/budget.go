package taskbudget

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrExceeded = errors.New("任务额度已用完或已有任务正在执行，请稍后重试")
var ErrUnavailable = errors.New("任务额度检查暂时不可用，请稍后重试")

type Policy struct {
	Daily, GlobalDaily, Concurrent, GlobalConcurrent int
	Lease                                            time.Duration
}
type Store struct {
	db       *sql.DB
	observer func(int, string, string, *time.Time)
}

func (s *Store) SetObserver(f func(int, string, string, *time.Time)) { s.observer = f }
func (s *Store) denied(owner int, bucket, reason string, retry *time.Time) {
	if s.observer != nil {
		s.observer(owner, bucket, reason, retry)
	}
}

func New(db *sql.DB) *Store { return &Store{db: db} }

// Acquire commits accounting BEFORE work begins. Its release only clears the
// concurrency lease; attempts never refund daily budget on rollback/failure.
func (s *Store) Acquire(parent context.Context, owner int, bucket string, cost int, p Policy) (func(), error) {
	if s == nil || s.db == nil || owner < 0 || bucket == "" || cost < 1 || p.Daily < 1 || p.GlobalDaily < 1 || p.Concurrent < 1 || p.GlobalConcurrent < 1 || p.Lease <= 0 {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	// One lock per category, always before counters or lease inspection. All
	// service processes share the lock and UTC date from the database clock.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "rss-task-budget:"+bucket); err != nil {
		return nil, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM task_budget_leases WHERE bucket=$1 AND expires_at<=clock_timestamp()`, bucket); err != nil {
		return nil, ErrUnavailable
	}
	var total, own int
	if err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE owner_id=$2) FROM task_budget_leases WHERE bucket=$1`, bucket, owner).Scan(&total, &own); err != nil {
		return nil, ErrUnavailable
	}
	if total >= p.GlobalConcurrent {
		s.denied(owner, bucket, "global_concurrency", nil)
		return nil, ErrExceeded
	}
	if owner > 0 && own >= p.Concurrent {
		s.denied(owner, bucket, "user_concurrency", nil)
		return nil, ErrExceeded
	}
	var day string
	if err = tx.QueryRowContext(ctx, `SELECT (clock_timestamp() AT TIME ZONE 'UTC')::date::text`).Scan(&day); err != nil {
		return nil, ErrUnavailable
	}
	var allUsed, userUsed int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(used) FILTER(WHERE owner_id=0),0),COALESCE(max(used) FILTER(WHERE owner_id=$2),0) FROM task_budget_daily WHERE bucket=$1 AND day=$3::date`, bucket, owner, day).Scan(&allUsed, &userUsed); err != nil {
		return nil, ErrUnavailable
	}
	reset, _ := time.Parse("2006-01-02", day)
	reset = reset.Add(24 * time.Hour)
	if cost > p.GlobalDaily-allUsed {
		s.denied(owner, bucket, "global_daily", &reset)
		return nil, ErrExceeded
	}
	if owner > 0 && cost > p.Daily-userUsed {
		s.denied(owner, bucket, "user_daily", &reset)
		return nil, ErrExceeded
	}
	owners := []int{0}
	if owner > 0 {
		owners = append(owners, owner)
	}
	for _, id := range owners {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES($1,$2,$4::date,$3) ON CONFLICT(owner_id,bucket,day) DO UPDATE SET used=task_budget_daily.used+EXCLUDED.used`, id, bucket, cost, day); err != nil {
			return nil, ErrUnavailable
		}
	}
	var raw [16]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return nil, ErrUnavailable
	}
	id := hex.EncodeToString(raw[:])
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_budget_leases(id,owner_id,bucket,expires_at) VALUES($1,$2,$3,clock_timestamp()+make_interval(secs=>$4))`, id, owner, bucket, p.Lease.Seconds()); err != nil {
		return nil, ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return nil, ErrUnavailable
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_, _ = s.db.ExecContext(releaseCtx, `DELETE FROM task_budget_leases WHERE id=$1`, id)
		})
	}, nil
}

type ownerKey struct{}

func WithOwner(ctx context.Context, id int) context.Context {
	return context.WithValue(ctx, ownerKey{}, id)
}
func Owner(ctx context.Context) int { v, _ := ctx.Value(ownerKey{}).(int); return v }
func IsDenied(err error) bool       { return errors.Is(err, ErrExceeded) || errors.Is(err, ErrUnavailable) }
func EnvError(name string) error    { return fmt.Errorf("%s must be a positive integer", name) }
