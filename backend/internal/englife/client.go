// Package englife integrates the englife web client protocol. These are private
// website endpoints, not an advertised stable API; fail closed on schema drift.
package englife

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrExpired     = errors.New("englife login expired")
	ErrQuota       = errors.New("englife quota exhausted")
	ErrUnavailable = errors.New("englife unavailable")
	ErrPending     = errors.New("englife task pending")
	ErrFailed      = errors.New("englife task needs attention")
	ErrPair        = errors.New("englife authorization expired or invalid")
	ErrDisabled    = errors.New("englife is not configured")
)
var videoPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var remoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Cookie struct {
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Domain         string  `json:"domain"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	ExpirationDate float64 `json:"expirationDate,omitempty"`
}
type Client struct{ baseURL string }

func NewClient() *Client { return &Client{baseURL: "https://englife.space"} }

type Session struct {
	client  *http.Client
	baseURL string
	csrf    string
	cookies []Cookie
}

func (c *Client) Session(cookies []Cookie) (*Session, error) {
	if len(cookies) == 0 || len(cookies) > 80 {
		return nil, ErrExpired
	}
	base, _ := url.Parse(c.baseURL)
	jar, _ := cookiejar.New(nil)
	total := 0
	var out []*http.Cookie
	for _, v := range cookies {
		if v.Domain != "englife.space" && v.Domain != ".englife.space" {
			return nil, ErrExpired
		}
		total += len(v.Name) + len(v.Value)
		if total > 24*1024 || v.Path != "/" && (!strings.HasPrefix(v.Path, "/") || strings.ContainsAny(v.Path, "\r\n;")) {
			return nil, ErrExpired
		}
		ck := &http.Cookie{Name: v.Name, Value: v.Value, Path: v.Path, Secure: v.Secure && base.Scheme == "https"}
		if ck.Valid() != nil || v.Name == "" {
			return nil, ErrExpired
		}
		if v.ExpirationDate > 0 {
			ck.Expires = time.Unix(int64(v.ExpirationDate), 0)
			if ck.Expires.Before(time.Now()) {
				continue
			}
		}
		out = append(out, ck)
	}
	if len(out) == 0 {
		return nil, ErrExpired
	}
	jar.SetCookies(base, out)
	return &Session{baseURL: c.baseURL, cookies: append([]Cookie(nil), cookies...), client: &http.Client{Timeout: 15 * time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *Session) request(ctx context.Context, method, path string, body any, out any) error {
	var b io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return ErrUnavailable
		}
		b = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, b)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 RSS-Pal/englife-integration")
	if method != "GET" {
		if s.csrf == "" {
			return ErrExpired
		}
		req.Header.Set("X-CSRF-Token", s.csrf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", s.baseURL)
		req.Header.Set("Referer", s.baseURL+"/en/")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	s.rememberCookies(req, resp.Cookies())
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil || len(data) > 8*1024*1024 {
		return ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if resp.StatusCode == 429 || e.Error.Code == "DAILY_GENERATION_LIMIT" || e.Error.Code == "GENERATION_QUOTA_PENDING" {
			return ErrQuota
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return ErrExpired
		}
		return ErrUnavailable
	}
	if err = json.Unmarshal(data, out); err != nil {
		return ErrUnavailable
	}
	return nil
}

type User struct {
	ID      string
	Account string
}

func (s *Session) Auth(ctx context.Context) (User, error) {
	var res struct {
		User *struct {
			ID    json.RawMessage `json:"id"`
			Email string          `json:"email"`
			Name  string          `json:"name"`
		} `json:"user"`
		CSRF string `json:"csrfToken"`
	}
	if err := s.request(ctx, "GET", "/api/auth/me", nil, &res); err != nil {
		return User{}, err
	}
	if res.User == nil || len(res.User.ID) == 0 || string(res.User.ID) == "null" || res.CSRF == "" {
		return User{}, ErrExpired
	}
	id := strings.Trim(string(res.User.ID), `"`)
	if len(id) > 200 || id == "" {
		return User{}, ErrUnavailable
	}
	account := res.User.Email
	if account == "" {
		account = res.User.Name
	}
	if account == "" {
		account = id
	}
	if len(account) > 320 {
		return User{}, ErrUnavailable
	}
	s.csrf = res.CSRF
	return User{id, account}, nil
}

type Quota struct {
	LimitsEnabled *bool `json:"limitsEnabled"`
	Unlimited     bool  `json:"unlimited"`
	Remaining     *int  `json:"remaining"`
}

func (s *Session) Quota(ctx context.Context) (Quota, error) {
	var q Quota
	err := s.request(ctx, "GET", "/api/quota", nil, &q)
	if err != nil {
		return q, err
	}
	if q.LimitsEnabled == nil {
		return q, ErrUnavailable
	}
	if *q.LimitsEnabled && !q.Unlimited {
		if q.Remaining == nil {
			return q, ErrUnavailable
		}
		if *q.Remaining <= 0 {
			return q, ErrQuota
		}
	}
	return q, nil
}

type RemoteJob struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	TargetLanguage string `json:"targetLanguage"`
	Status         string `json:"status"`
	Video          struct {
		ID string `json:"id"`
	} `json:"video"`
}

func (s *Session) Jobs(ctx context.Context) ([]RemoteJob, error) {
	var jobs []RemoteJob
	err := s.request(ctx, "GET", "/api/jobs", nil, &jobs)
	return jobs, err
}
func (s *Session) Submit(ctx context.Context, video string) (string, error) {
	if !videoPattern.MatchString(video) {
		return "", ErrFailed
	}
	var r struct {
		ID string `json:"id"`
	}
	err := s.request(ctx, "POST", "/api/jobs", map[string]string{"url": "https://www.youtube.com/watch?v=" + video, "targetLanguage": "zh"}, &r)
	if err != nil {
		return "", err
	}
	if !remoteIDPattern.MatchString(r.ID) {
		return "", ErrUnavailable
	}
	return r.ID, nil
}
func (s *Session) Document(ctx context.Context, id, video string) (string, error) {
	if !remoteIDPattern.MatchString(id) {
		return "", ErrFailed
	}
	var preview struct {
		Status string `json:"status"`
	}
	path := "/api/documents/" + url.PathEscape(id)
	if err := s.request(ctx, "GET", path+"/preview", nil, &preview); err != nil {
		return "", err
	}
	switch preview.Status {
	case "ready":
	case "failed":
		return "", ErrFailed
	case "":
		return "", ErrUnavailable
	default:
		return "", ErrPending
	}
	var doc Document
	if err := s.request(ctx, "GET", path, nil, &doc); err != nil {
		return "", err
	}
	return doc.Markdown(video)
}

type Document struct {
	Video struct {
		ID string `json:"id"`
	} `json:"video"`
	TargetLanguage string `json:"targetLanguage"`
	OneLine        string `json:"oneLine"`
	Guide          []struct {
		Text string `json:"text"`
	} `json:"guide"`
	Chapters []struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		ParagraphIDs []string `json:"paragraphIds"`
	} `json:"chapters"`
	Segments []struct {
		ID             string  `json:"id"`
		Start          float64 `json:"start"`
		TranslatedText string  `json:"translatedText"`
	} `json:"segments"`
}

func (d Document) Markdown(video string) (string, error) {
	if !videoPattern.MatchString(video) || d.Video.ID != video || d.TargetLanguage != "zh" || len(d.Chapters) == 0 || len(d.Segments) == 0 {
		return "", ErrUnavailable
	}
	index := map[string]int{}
	for i, s := range d.Segments {
		if s.ID == "" || strings.TrimSpace(s.TranslatedText) == "" || s.Start < 0 {
			return "", ErrUnavailable
		}
		if _, ok := index[s.ID]; ok {
			return "", ErrUnavailable
		}
		index[s.ID] = i
	}
	var b strings.Builder
	if d.OneLine != "" {
		b.WriteString(d.OneLine + "\n\n")
	}
	for _, g := range d.Guide {
		if g.Text != "" {
			b.WriteString(g.Text + "\n\n")
		}
	}
	seen := map[string]bool{}
	for _, chapter := range d.Chapters {
		if len(chapter.ParagraphIDs) == 0 {
			return "", ErrUnavailable
		}
		b.WriteString("### " + strings.ReplaceAll(chapter.Title, "\n", " ") + "\n\n")
		for _, id := range chapter.ParagraphIDs {
			i, ok := index[id]
			if !ok {
				return "", ErrUnavailable
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			s := d.Segments[i]
			fmt.Fprintf(&b, "[原视频 %d:%02d](https://www.youtube.com/watch?v=%s&t=%d)\n\n%s\n\n", int(s.Start)/60, int(s.Start)%60, video, int(s.Start), s.TranslatedText)
		}
	}
	if len(seen) != len(d.Segments) {
		return "", ErrUnavailable
	}
	return strings.TrimSpace(b.String()), nil
}

// Preserve Set-Cookie rotations for the next worker process. Restrict updates to
// the same provider domain; credentials never follow redirects or leave it.
func (s *Session) rememberCookies(req *http.Request, updates []*http.Cookie) {
	for _, v := range updates {
		domain := strings.TrimPrefix(v.Domain, ".")
		if domain != "" && domain != "englife.space" {
			continue
		}
		path := v.Path
		if path == "" {
			i := strings.LastIndex(req.URL.Path, "/")
			if i > 0 {
				path = req.URL.Path[:i]
			} else {
				path = "/"
			}
		}
		out := s.cookies[:0]
		for _, c := range s.cookies {
			if !(c.Name == v.Name && c.Path == path) {
				out = append(out, c)
			}
		}
		s.cookies = out
		if v.MaxAge < 0 || (!v.Expires.IsZero() && v.Expires.Before(time.Now())) {
			continue
		}
		c := Cookie{Name: v.Name, Value: v.Value, Domain: "englife.space", Path: path, Secure: true}
		if !v.Expires.IsZero() {
			c.ExpirationDate = float64(v.Expires.Unix())
		}
		if v.MaxAge > 0 {
			c.ExpirationDate = float64(time.Now().Unix() + int64(v.MaxAge))
		}
		s.cookies = append(s.cookies, c)
	}
}
