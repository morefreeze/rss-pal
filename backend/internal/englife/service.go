package englife

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
)

type Connection struct {
	Secret                    []byte
	AccountID, Account, State string
	ConnectionID              string
	CheckedAt                 time.Time
	Remaining                 *int
}
type Job struct {
	AccountID string    `json:"-"`
	VideoID   string    `json:"video_id"`
	RemoteID  string    `json:"-"`
	Status    string    `json:"status"`
	Output    string    `json:"-"`
	NextCheck time.Time `json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Store interface {
	Lock(context.Context) (func(), error)
	Connection(context.Context) (Connection, error)
	SaveConnection(context.Context, Connection) error
	Disconnect(context.Context) error
	CreatePair(context.Context, string, int, time.Time) error
	PairAdmin(context.Context, string) (int, error)
	CompletePair(context.Context, string, Connection) error
	Job(context.Context, string, string) (Job, error)
	SaveJob(context.Context, Job) error
	Jobs(context.Context, string) ([]Job, error)
}
type Service struct {
	store  Store
	vault  *vault
	client *Client
}

func NewService(store Store, key string) (*Service, error) {
	s := &Service{store: store, client: NewClient()}
	if key != "" {
		v, err := newVault(key)
		if err != nil {
			return nil, err
		}
		s.vault = v
	}
	return s, nil
}

type Status struct {
	Configured   bool       `json:"configured"`
	State        string     `json:"state"`
	ConnectionID string     `json:"connection_id,omitempty"`
	Account      string     `json:"account,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	Remaining    *int       `json:"remaining,omitempty"`
	Jobs         []Job      `json:"jobs"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	st := Status{Configured: s.vault != nil, State: "setup_required", Jobs: []Job{}}
	if s.vault == nil {
		return st, nil
	}
	c, err := s.store.Connection(ctx)
	if err != nil {
		return st, err
	}
	st.State = "disconnected"
	if len(c.Secret) > 0 {
		st.State = c.State
		st.Account = c.Account
		st.ConnectionID = c.ConnectionID
		st.CheckedAt = &c.CheckedAt
		st.Remaining = c.Remaining
		st.Jobs, err = s.store.Jobs(ctx, c.AccountID)
	}
	return st, err
}

type Pair struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (s *Service) Pair(ctx context.Context, admin int) (Pair, error) {
	if s.vault == nil {
		return Pair{}, ErrDisabled
	}
	unlock, err := s.store.Lock(ctx)
	if err != nil {
		return Pair{}, err
	}
	defer unlock()
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return Pair{}, err
	}
	p := Pair{base64.RawURLEncoding.EncodeToString(b), time.Now().Add(10 * time.Minute)}
	err = s.store.CreatePair(ctx, hashToken(p.Token), admin, p.ExpiresAt)
	return p, err
}
func (s *Service) Complete(ctx context.Context, token string, cookies []Cookie) error {
	if s.vault == nil {
		return ErrDisabled
	}
	if len(token) != 43 {
		return ErrPair
	}
	unlock, err := s.store.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	hash := hashToken(token)
	if _, err = s.store.PairAdmin(ctx, hash); err != nil {
		return ErrPair
	}
	remote, err := s.client.Session(cookies)
	if err != nil {
		return err
	}
	u, err := remote.Auth(ctx)
	if err != nil {
		return err
	}
	q, err := remote.Quota(ctx)
	if err != nil && !errors.Is(err, ErrQuota) {
		return err
	}
	encrypted, e := s.vault.seal(remote.cookies)
	if e != nil {
		return e
	}
	connectionID := make([]byte, 18)
	if _, e = rand.Read(connectionID); e != nil {
		return e
	}
	c := Connection{ConnectionID: base64.RawURLEncoding.EncodeToString(connectionID), Secret: encrypted, AccountID: u.ID, Account: u.Account, State: connectionState(err), CheckedAt: time.Now(), Remaining: q.Remaining}
	return s.store.CompletePair(ctx, hash, c)
}
func (s *Service) Disconnect(ctx context.Context) error {
	unlock, err := s.store.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.store.Disconnect(ctx)
}
func (s *Service) Check(ctx context.Context) (Status, error) {
	if s.vault == nil {
		return s.Status(ctx)
	}
	unlock, err := s.store.Lock(ctx)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	c, err := s.store.Connection(ctx)
	if err != nil {
		return Status{}, err
	}
	if len(c.Secret) == 0 {
		return s.Status(ctx)
	}
	remote, err := s.session(c)
	var q Quota
	if err == nil {
		var u User
		u, err = remote.Auth(ctx)
		if err == nil {
			if u.ID != c.AccountID {
				err = ErrExpired
			} else {
				c.Account = u.Account
				q, err = remote.Quota(ctx)
			}
		}
	}
	if remote != nil {
		if secret, e := s.vault.seal(remote.cookies); e == nil {
			c.Secret = secret
		} else {
			return Status{}, e
		}
	}
	c.State = connectionState(err)
	c.CheckedAt = time.Now()
	c.Remaining = q.Remaining
	if e := s.store.SaveConnection(ctx, c); e != nil {
		return Status{}, e
	}
	return s.Status(ctx)
}
func connectionState(err error) string {
	switch {
	case err == nil:
		return "ready"
	case errors.Is(err, ErrExpired):
		return "expired"
	case errors.Is(err, ErrQuota):
		return "quota_exhausted"
	default:
		return "unavailable"
	}
}
func (s *Service) session(c Connection) (*Session, error) {
	cookies, err := s.vault.open(c.Secret)
	if err != nil {
		return nil, err
	}
	return s.client.Session(cookies)
}

// Fetch performs one bounded step. The durable submitting state is committed
// before any POST, so a lost response or process crash cannot cause a blind retry.
func (s *Service) Fetch(ctx context.Context, video string) (output string, resultErr error) {
	if s.vault == nil {
		return "", nil
	}
	if !videoPattern.MatchString(video) {
		return "", ErrFailed
	}
	unlock, err := s.store.Lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	c, err := s.store.Connection(ctx)
	if err != nil {
		return "", err
	}
	if len(c.Secret) == 0 {
		return "", nil
	}
	j, err := s.store.Job(ctx, c.AccountID, video)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if exists && j.Status == "ready" {
		return j.Output, nil
	}
	switch c.State {
	case "expired":
		return "", ErrExpired
	case "quota_exhausted":
		if !exists || j.Status == "new" {
			return "", ErrQuota
		}
	case "unavailable":
		if time.Since(c.CheckedAt) < 5*time.Minute {
			return "", ErrUnavailable
		}
	}
	if exists && (j.NextCheck.After(time.Now()) || j.Status == "failed") {
		return "", ErrPending
	}
	remote, err := s.session(c)
	if remote != nil {
		defer func() {
			latest, e := s.store.Connection(ctx)
			if e == nil {
				var secret []byte
				secret, e = s.vault.seal(remote.cookies)
				if e == nil {
					latest.Secret = secret
					e = s.store.SaveConnection(ctx, latest)
				}
			}
			if e != nil {
				output = ""
				resultErr = ErrUnavailable
			}
		}()
	}
	if err == nil {
		var u User
		u, err = remote.Auth(ctx)
		if err == nil && u.ID != c.AccountID {
			err = ErrExpired
		}
	}
	if err != nil {
		return "", s.pause(ctx, c, err)
	}
	// Recover health after the cooldown, without charging a new generation.
	if c.State == "unavailable" {
		q, e := remote.Quota(ctx)
		if e != nil && !errors.Is(e, ErrQuota) {
			return "", s.pause(ctx, c, e)
		}
		c.State = connectionState(e)
		c.Remaining = q.Remaining
	}
	c.CheckedAt = time.Now()
	if e := s.store.SaveConnection(ctx, c); e != nil {
		return "", e
	}
	if !exists {
		j = Job{AccountID: c.AccountID, VideoID: video, Status: "new"}
	}
	if j.RemoteID == "" {
		jobs, e := remote.Jobs(ctx)
		if e != nil {
			return "", s.pause(ctx, c, e)
		}
		for _, v := range jobs {
			if v.TargetLanguage == "zh" && remoteVideo(v) == video && v.Status != "failed" && remoteIDPattern.MatchString(v.ID) {
				j.RemoteID = v.ID
				j.Status = "pending"
				break
			}
		}
		if j.RemoteID == "" {
			if j.Status != "new" {
				j.Status = "needs_attention"
				j.NextCheck = time.Now().Add(10 * time.Minute)
				if e = s.saveJob(ctx, &j); e != nil {
					return "", e
				}
				return "", ErrFailed
			}
			if c.State == "quota_exhausted" {
				return "", ErrQuota
			}
			q, e := remote.Quota(ctx)
			c.Remaining = q.Remaining
			if e != nil {
				return "", s.pause(ctx, c, e)
			}
			c.State = "ready"
			c.CheckedAt = time.Now()
			if e = s.store.SaveConnection(ctx, c); e != nil {
				return "", e
			}
			j.Status = "submitting"
			j.NextCheck = time.Now().Add(time.Minute)
			if e = s.saveJob(ctx, &j); e != nil {
				return "", e
			}
			j.RemoteID, e = remote.Submit(ctx, video)
			if e != nil {
				// A definitive auth/quota refusal can be retried after a manual check.
				if errors.Is(e, ErrQuota) || errors.Is(e, ErrExpired) {
					j.Status = "new"
				}
				if saveErr := s.saveJob(ctx, &j); saveErr != nil {
					return "", saveErr
				}
				return "", s.pause(ctx, c, e)
			}
			j.Status = "pending"
			if e = s.saveJob(ctx, &j); e != nil {
				return "", e
			}
			return "", ErrPending
		}
	}
	text, e := remote.Document(ctx, j.RemoteID, video)
	j.NextCheck = time.Now().Add(time.Minute)
	switch {
	case e == nil:
		j.Status = "ready"
		j.Output = text
	case errors.Is(e, ErrPending):
		j.Status = "pending"
	case errors.Is(e, ErrFailed):
		j.Status = "failed"
	default:
		j.NextCheck = time.Now().Add(5 * time.Minute)
	}
	if saveErr := s.saveJob(ctx, &j); saveErr != nil {
		return "", saveErr
	}
	if e != nil && !errors.Is(e, ErrPending) && !errors.Is(e, ErrFailed) {
		return "", s.pause(ctx, c, e)
	}
	return text, e
}
func (s *Service) saveJob(ctx context.Context, j *Job) error {
	j.UpdatedAt = time.Now()
	return s.store.SaveJob(ctx, *j)
}
func (s *Service) pause(ctx context.Context, c Connection, cause error) error {
	c.State = connectionState(cause)
	c.CheckedAt = time.Now()
	if err := s.store.SaveConnection(ctx, c); err != nil {
		return err
	}
	return cause
}
func remoteVideo(j RemoteJob) string {
	if videoPattern.MatchString(j.Video.ID) {
		return j.Video.ID
	}
	u, err := url.Parse(j.URL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "youtu.be" {
		return strings.TrimPrefix(u.Path, "/")
	}
	if host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" {
		return u.Query().Get("v")
	}
	return ""
}

// Connected is a read-only eligibility check; it never contacts the provider.
func (s *Service) Connected(ctx context.Context) (bool, error) {
	if s.vault == nil {
		return false, nil
	}
	c, err := s.store.Connection(ctx)
	return len(c.Secret) > 0, err
}
