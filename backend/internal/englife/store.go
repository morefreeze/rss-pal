package englife

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db} }

// Session lock spans independently committed writes and upstream calls. Waiting
// workers return immediately; they must not hold all database pool connections.
func (s *SQLStore) Lock(ctx context.Context) (func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext(current_schema()),721004892)`).Scan(&ok); err != nil || !ok {
		conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrPending
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock(hashtext(current_schema()),721004892)`).Scan(&released); err != nil || !released {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
	}, nil
}
func (s *SQLStore) Connection(ctx context.Context) (Connection, error) {
	var c Connection
	err := s.db.QueryRowContext(ctx, `SELECT secret,account_id,account,state,checked_at,remaining,connection_id FROM englife_connection WHERE id=true`).Scan(&c.Secret, &c.AccountID, &c.Account, &c.State, &c.CheckedAt, &c.Remaining, &c.ConnectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	return c, err
}

const saveConnectionSQL = `INSERT INTO englife_connection(id,secret,account_id,account,state,checked_at,remaining,connection_id) VALUES(true,$1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET secret=EXCLUDED.secret,account_id=EXCLUDED.account_id,account=EXCLUDED.account,state=EXCLUDED.state,checked_at=EXCLUDED.checked_at,remaining=EXCLUDED.remaining,connection_id=EXCLUDED.connection_id`

func (s *SQLStore) SaveConnection(ctx context.Context, c Connection) error {
	_, err := s.db.ExecContext(ctx, saveConnectionSQL, c.Secret, c.AccountID, c.Account, c.State, c.CheckedAt, c.Remaining, c.ConnectionID)
	return err
}
func (s *SQLStore) Disconnect(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM englife_pairing`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM englife_connection`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLStore) CreatePair(ctx context.Context, hash string, admin int, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO englife_pairing(id,token_hash,admin_id,expires_at) VALUES(true,$1,$2,$3) ON CONFLICT(id) DO UPDATE SET token_hash=EXCLUDED.token_hash,admin_id=EXCLUDED.admin_id,expires_at=EXCLUDED.expires_at`, hash, admin, expires)
	return err
}
func (s *SQLStore) PairAdmin(ctx context.Context, hash string) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx, `SELECT p.admin_id FROM englife_pairing p JOIN users u ON u.id=p.admin_id WHERE p.token_hash=$1 AND p.expires_at>now() AND u.is_admin`, hash).Scan(&id)
	if err != nil {
		return 0, ErrPair
	}
	return id, nil
}
func (s *SQLStore) CompletePair(ctx context.Context, hash string, c Connection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int
	if err = tx.QueryRowContext(ctx, `DELETE FROM englife_pairing p USING users u WHERE p.token_hash=$1 AND p.expires_at>now() AND p.admin_id=u.id AND u.is_admin RETURNING p.admin_id`, hash).Scan(&id); err != nil {
		return ErrPair
	}
	if _, err = tx.ExecContext(ctx, saveConnectionSQL, c.Secret, c.AccountID, c.Account, c.State, c.CheckedAt, c.Remaining, c.ConnectionID); err != nil {
		return err
	}
	// Prior no-caption attempts become eligible when a new provider is connected.
	if _, err = tx.ExecContext(ctx, `UPDATE articles SET transcript_fetched_at=NULL,transcript_next_attempt_at=NULL WHERE media_type='video/youtube' AND COALESCE(content,'') !~ '(^|\n)## (字幕|视频整理（englife）)'`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLStore) Job(ctx context.Context, account, video string) (Job, error) {
	var j Job
	j.AccountID = account
	j.VideoID = video
	err := s.db.QueryRowContext(ctx, `SELECT remote_id,status,output,next_check_at,updated_at FROM englife_jobs WHERE account_id=$1 AND video_id=$2 AND language='zh'`, account, video).Scan(&j.RemoteID, &j.Status, &j.Output, &j.NextCheck, &j.UpdatedAt)
	return j, err
}
func (s *SQLStore) SaveJob(ctx context.Context, j Job) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO englife_jobs(account_id,video_id,language,remote_id,status,output,next_check_at,updated_at) VALUES($1,$2,'zh',$3,$4,$5,$6,$7) ON CONFLICT(account_id,video_id,language) DO UPDATE SET remote_id=EXCLUDED.remote_id,status=EXCLUDED.status,output=EXCLUDED.output,next_check_at=EXCLUDED.next_check_at,updated_at=EXCLUDED.updated_at`, j.AccountID, j.VideoID, j.RemoteID, j.Status, j.Output, j.NextCheck, j.UpdatedAt)
	return err
}
func (s *SQLStore) Jobs(ctx context.Context, account string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT video_id,status,updated_at FROM englife_jobs WHERE account_id=$1 ORDER BY updated_at DESC LIMIT 20`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if err = rows.Scan(&j.VideoID, &j.Status, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
