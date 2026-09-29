package englife

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionCSRFQuotaAndDocument(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=secret" {
			t.Error("missing scoped session")
		}
		switch r.URL.Path {
		case "/api/auth/me":
			w.Write([]byte(`{"user":{"id":"u1","email":"a@example.org"},"csrfToken":"csrf"}`))
		case "/api/quota":
			w.Write([]byte(`{"limitsEnabled":true,"unlimited":false,"remaining":2}`))
		case "/api/jobs":
			if r.Method == "POST" {
				posted = true
				if r.Header.Get("X-CSRF-Token") != "csrf" {
					t.Error("missing csrf")
				}
				var b map[string]string
				json.NewDecoder(r.Body).Decode(&b)
				if b["url"] != "https://www.youtube.com/watch?v=abcdefghijk" || b["targetLanguage"] != "zh" {
					t.Errorf("bad body: %v", b)
				}
				w.Write([]byte(`{"id":"j1"}`))
			} else {
				w.Write([]byte(`[]`))
			}
		case "/api/documents/j1/preview":
			w.Write([]byte(`{"status":"ready"}`))
		case "/api/documents/j1":
			w.Write([]byte(`{"video":{"id":"abcdefghijk"},"targetLanguage":"zh","oneLine":"Overview","guide":[{"text":"Guide"}],"chapters":[{"id":"c1","title":"Chapter","paragraphIds":["p1"]}],"segments":[{"id":"p1","start":12,"translatedText":"Translated text","sourceText":"Original"}]}`))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := testClient(srv.URL)
	s, err := c.Session([]Cookie{{Name: "session", Value: "secret", Domain: "englife.space", Path: "/", Secure: true}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Auth(context.Background())
	if err != nil || u.ID != "u1" {
		t.Fatalf("auth: %v %v", u, err)
	}
	q, err := s.Quota(context.Background())
	if err != nil || q.Remaining == nil || *q.Remaining != 2 {
		t.Fatalf("quota %v %v", q, err)
	}
	id, err := s.Submit(context.Background(), "abcdefghijk")
	if err != nil || id != "j1" || !posted {
		t.Fatalf("submit %s %v", id, err)
	}
	text, err := s.Document(context.Background(), id, "abcdefghijk")
	if err != nil || !strings.Contains(text, "Translated text") || !strings.Contains(text, "t=12") {
		t.Fatalf("document %q %v", text, err)
	}
}
func TestSessionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want error
	}{{"expired", 401, `{"secret":"never expose"}`, ErrExpired}, {"quota", 429, `{}`, ErrQuota}, {"daily", 403, `{"error":{"code":"DAILY_GENERATION_LIMIT"}}`, ErrQuota}, {"server", 500, `{"secret":"never expose"}`, ErrUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); w.Write([]byte(tc.body)) }))
			defer srv.Close()
			s, _ := testClient(srv.URL).Session(validCookies())
			_, err := s.Auth(context.Background())
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error %v", err)
			}
		})
	}
}
func TestSessionRejectsRedirectAndMalformedDocuments(t *testing.T) {
	leaked := false
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer dest.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 302) }))
	defer srv.Close()
	s, _ := testClient(srv.URL).Session(validCookies())
	_, err := s.Auth(context.Background())
	if err == nil || leaked {
		t.Fatal("followed redirect")
	}
	for _, body := range []string{`{}`, `{"video":{"id":"wrong"},"targetLanguage":"zh","chapters":[],"segments":[]}`, `{"video":{"id":"abcdefghijk"},"targetLanguage":"zh","chapters":[{"paragraphIds":["missing"]}],"segments":[{"id":"other","translatedText":"text"}]}`} {
		var d Document
		json.Unmarshal([]byte(body), &d)
		if _, err := d.Markdown("abcdefghijk"); err == nil {
			t.Fatal("accepted incomplete/mismatched document")
		}
	}
}
func TestCookieScopeAndEncryption(t *testing.T) {
	for _, c := range [][]Cookie{{{Name: "SID", Value: "x", Domain: "google.com", Path: "/"}}, {{Name: "session", Value: "x\r\nAuthorization: bad", Domain: "englife.space", Path: "/"}}, {{Name: "session", Value: "x", Domain: "evil.englife.space", Path: "/"}}, nil} {
		if _, err := NewClient().Session(c); err == nil {
			t.Fatal("accepted unsafe cookies")
		}
	}
	box, err := newVault(strings.Repeat("a", 43) + "=")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := box.seal(validCookies())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "secret") {
		t.Fatal("plaintext persisted")
	}
	got, err := box.open(encrypted)
	if err != nil || got[0].Value != "secret" {
		t.Fatalf("decrypt %v", err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := box.open(encrypted); err == nil {
		t.Fatal("accepted modified ciphertext")
	}
}
func validCookies() []Cookie {
	return []Cookie{{Name: "session", Value: "secret", Domain: "englife.space", Path: "/", Secure: true}}
}
func testClient(base string) *Client { c := NewClient(); c.baseURL = base; return c }

func TestRenewedSessionIsPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "renewed", Path: "/", HttpOnly: true})
		w.Write([]byte(`{"user":{"id":"u1"},"csrfToken":"csrf"}`))
	}))
	defer srv.Close()
	s, _ := testClient(srv.URL).Session(validCookies())
	if _, err := s.Auth(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := s.cookies
	if len(got) != 1 || got[0].Value != "renewed" {
		t.Fatalf("renewed session lost: %d", len(got))
	}
}
