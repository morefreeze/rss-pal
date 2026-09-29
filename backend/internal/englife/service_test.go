package englife

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu      sync.Mutex
	c       Connection
	pair    string
	expires time.Time
	admin   bool
	jobs    map[string]Job
}

func (m *memoryStore) Lock(context.Context) (func(), error)                 { m.mu.Lock(); return m.mu.Unlock, nil }
func (m *memoryStore) Connection(context.Context) (Connection, error)       { return m.c, nil }
func (m *memoryStore) SaveConnection(_ context.Context, c Connection) error { m.c = c; return nil }
func (m *memoryStore) Disconnect(context.Context) error                     { m.c = Connection{}; m.pair = ""; return nil }
func (m *memoryStore) CreatePair(_ context.Context, hash string, admin int, expires time.Time) error {
	m.pair = hash
	m.expires = expires
	return nil
}
func (m *memoryStore) PairAdmin(_ context.Context, hash string) (int, error) {
	if hash != m.pair || !m.admin || !m.expires.After(time.Now()) {
		return 0, ErrPair
	}
	return 1, nil
}
func (m *memoryStore) CompletePair(_ context.Context, hash string, c Connection) error {
	if hash != m.pair {
		return ErrPair
	}
	m.pair = ""
	m.c = c
	return nil
}
func (m *memoryStore) Job(_ context.Context, account, video string) (Job, error) {
	j, ok := m.jobs[account+video]
	if !ok {
		return Job{}, sql.ErrNoRows
	}
	return j, nil
}
func (m *memoryStore) SaveJob(_ context.Context, j Job) error {
	m.jobs[j.AccountID+j.VideoID] = j
	return nil
}
func (m *memoryStore) Jobs(_ context.Context, account string) ([]Job, error) {
	j := []Job{}
	for _, v := range m.jobs {
		if v.AccountID == account {
			j = append(j, v)
		}
	}
	return j, nil
}
func newTestService(t *testing.T, h http.HandlerFunc) (*Service, *memoryStore) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	m := &memoryStore{admin: true, jobs: map[string]Job{}}
	s, err := NewService(m, strings.Repeat("a", 43)+"=")
	if err != nil {
		t.Fatal(err)
	}
	s.client = testClient(srv.URL)
	return s, m
}
func authFixture(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/auth/me":
		w.Write([]byte(`{"user":{"id":"u1","email":"admin@example.org"},"csrfToken":"csrf"}`))
	case "/api/quota":
		w.Write([]byte(`{"limitsEnabled":true,"remaining":2}`))
	default:
		return false
	}
	return true
}
func connectTest(t *testing.T, s *Service) {
	t.Helper()
	p, err := s.Pair(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(context.Background(), p.Token, validCookies()); err != nil {
		t.Fatal(err)
	}
}
func TestPairSingleUseAndDisconnect(t *testing.T) {
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) { authFixture(w, r) })
	p, err := s.Pair(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.pair == p.Token {
		t.Fatal("stored raw token")
	}
	if err = s.Complete(context.Background(), p.Token, validCookies()); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(context.Background(), p.Token, validCookies()); !errors.Is(err, ErrPair) {
		t.Fatal("replay accepted")
	}
	st, err := s.Status(context.Background())
	if err != nil || st.State != "ready" || st.Account != "admin@example.org" || st.ConnectionID == "" {
		t.Fatalf("status %+v %v", st, err)
	}
	before := st.ConnectionID
	if _, err = s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Status(context.Background())
	if st.ConnectionID != before {
		t.Fatal("check changed connection identity")
	}
	connectTest(t, s)
	st, _ = s.Status(context.Background())
	if st.ConnectionID == before {
		t.Fatal("reauthorization did not change identity")
	}
	p, _ = s.Pair(context.Background(), 1)
	if err = s.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(context.Background(), p.Token, validCookies()); !errors.Is(err, ErrPair) {
		t.Fatal("disconnect did not revoke pairing")
	}
	for _, mode := range []string{"expired", "demoted"} {
		p, _ = s.Pair(context.Background(), 1)
		if mode == "expired" {
			m.expires = time.Now().Add(-time.Second)
		} else {
			m.admin = false
		}
		if err = s.Complete(context.Background(), p.Token, validCookies()); !errors.Is(err, ErrPair) {
			t.Fatal(mode)
		}
	}
}
func TestPersistentPendingAndAmbiguousSubmission(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{true: "ambiguous", false: "pending"}[ambiguous], func(t *testing.T) {
			posts := 0
			listed := false
			documents := 0
			s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if authFixture(w, r) {
					return
				}
				switch r.URL.Path {
				case "/api/jobs":
					if r.Method == "POST" {
						posts++
						if ambiguous {
							w.WriteHeader(502)
						} else {
							w.Write([]byte(`{"id":"j1"}`))
						}
					} else {
						if listed {
							w.Write([]byte(`[{"id":"j1","url":"https://www.youtube.com/watch?v=abcdefghijk","targetLanguage":"zh","status":"ready"}]`))
						} else {
							w.Write([]byte(`[]`))
						}
					}
				case "/api/documents/j1/preview":
					documents++
					w.Write([]byte(`{"status":"generating"}`))
				default:
					t.Error(r.URL.Path)
				}
			})
			connectTest(t, s)
			_, err := s.Fetch(context.Background(), "abcdefghijk")
			if err == nil {
				t.Fatal("returned incomplete text")
			}
			listed = true
			m.c.CheckedAt = time.Now().Add(-6 * time.Minute)
			// New service models an API/worker restart; submitted state survives.
			s2, _ := NewService(m, strings.Repeat("a", 43)+"=")
			s2.client = s.client
			for k, j := range m.jobs {
				j.NextCheck = time.Time{}
				m.jobs[k] = j
			}
			_, _ = s2.Fetch(context.Background(), "abcdefghijk")
			if documents != 1 {
				t.Fatalf("expected reconciled document fetch, got %d", documents)
			}
			if posts != 1 {
				t.Fatalf("duplicate submission %d", posts)
			}
		})
	}
}
func TestPausedAccountDoesNotCallUpstream(t *testing.T) {
	calls := 0
	expired := false
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if expired {
			w.WriteHeader(401)
			return
		}
		authFixture(w, r)
	})
	connectTest(t, s)
	expired = true
	_, _ = s.Check(context.Background())
	if m.c.State != "expired" {
		t.Fatal(m.c.State)
	}
	before := calls
	_, _ = s.Fetch(context.Background(), "abcdefghijk")
	_, _ = s.Status(context.Background())
	if calls != before {
		t.Fatal("paused account called upstream")
	}
}
func TestNoQuotaDoesNotSubmit(t *testing.T) {
	posts := 0
	noQuota := false
	s, _ := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		if r.URL.Path == "/api/quota" && noQuota {
			w.Write([]byte(`{"limitsEnabled":true,"remaining":0}`))
			return
		}
		if !authFixture(w, r) {
			w.Write([]byte(`[]`))
		}
	})
	connectTest(t, s)
	noQuota = true
	_, _ = s.Fetch(context.Background(), "abcdefghijk")
	if posts != 0 {
		t.Fatal("submitted without quota")
	}
}

func TestUnresolvedSubmissionNeedsAttentionWithoutResubmit(t *testing.T) {
	posts := 0
	lists := 0
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if authFixture(w, r) {
			return
		}
		if r.URL.Path == "/api/jobs" {
			if r.Method == "POST" {
				posts++
				w.WriteHeader(502)
			} else {
				lists++
				w.Write([]byte(`[]`))
			}
			return
		}
		t.Error(r.URL.Path)
	})
	connectTest(t, s)
	_, _ = s.Fetch(context.Background(), "abcdefghijk")
	m.c.CheckedAt = time.Now().Add(-6 * time.Minute)
	for k, j := range m.jobs {
		j.NextCheck = time.Time{}
		m.jobs[k] = j
	}
	_, err := s.Fetch(context.Background(), "abcdefghijk")
	if !errors.Is(err, ErrFailed) || posts != 1 || lists != 2 {
		t.Fatalf("bad reconciliation: err=%v posts=%d lists=%d", err, posts, lists)
	}
	if m.jobs["u1abcdefghijk"].Status != "needs_attention" {
		t.Fatal("unresolved outcome not visible")
	}
}
func TestReadyOutputSurvivesRestartWithoutUpstreamCalls(t *testing.T) {
	calls := 0
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) { calls++; authFixture(w, r) })
	connectTest(t, s)
	m.jobs["u1abcdefghijk"] = Job{AccountID: "u1", VideoID: "abcdefghijk", Status: "ready", Output: "cached article"}
	s2, _ := NewService(m, strings.Repeat("a", 43)+"=")
	s2.client = s.client
	before := calls
	text, err := s2.Fetch(context.Background(), "abcdefghijk")
	if err != nil || text != "cached article" || calls != before {
		t.Fatal("did not reuse cached output")
	}
}

func TestExhaustedQuotaStillCollectsAlreadySubmittedDocument(t *testing.T) {
	posts := 0
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		if authFixture(w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/documents/j1/preview":
			w.Write([]byte(`{"status":"ready"}`))
		case "/api/documents/j1":
			w.Write([]byte(`{"video":{"id":"abcdefghijk"},"targetLanguage":"zh","chapters":[{"paragraphIds":["p1"]}],"segments":[{"id":"p1","translatedText":"Already paid output"}]}`))
		default:
			t.Error(r.URL.Path)
		}
	})
	connectTest(t, s)
	m.c.State = "quota_exhausted"
	m.jobs["u1abcdefghijk"] = Job{AccountID: "u1", VideoID: "abcdefghijk", Status: "pending", RemoteID: "j1"}
	text, err := s.Fetch(context.Background(), "abcdefghijk")
	if err != nil || !strings.Contains(text, "Already paid output") || posts != 0 {
		t.Fatalf("cannot collect pending task %v %q", err, text)
	}
	if m.c.State != "quota_exhausted" {
		t.Fatal("cleared quota without checking")
	}
}

func TestExistingJobRecoveryRestoresConnectionHealth(t *testing.T) {
	s, m := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if authFixture(w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/documents/j1/preview":
			w.Write([]byte(`{"status":"ready"}`))
		case "/api/documents/j1":
			w.Write([]byte(`{"video":{"id":"abcdefghijk"},"targetLanguage":"zh","chapters":[{"paragraphIds":["p1"]}],"segments":[{"id":"p1","translatedText":"Recovered output"}]}`))
		default:
			t.Error(r.URL.Path)
		}
	})
	connectTest(t, s)
	m.c.State = "unavailable"
	m.c.CheckedAt = time.Now().Add(-6 * time.Minute)
	old := m.c.CheckedAt
	m.jobs["u1abcdefghijk"] = Job{AccountID: "u1", VideoID: "abcdefghijk", Status: "pending", RemoteID: "j1"}
	text, err := s.Fetch(context.Background(), "abcdefghijk")
	if err != nil || text == "" {
		t.Fatal(err)
	}
	if m.c.State != "ready" || !m.c.CheckedAt.After(old) {
		t.Fatal("health did not recover", m.c.State)
	}
}
