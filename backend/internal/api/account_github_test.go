package api

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGitHubLoginConfiguration(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_ORIGIN", "https://rss.morefreeze.top")
	t.Setenv("GITHUB_CLIENT_ID", "test-github-client")
	t.Setenv("GITHUB_CLIENT_SECRET", "test-github-secret")
	h := NewAccountAuthHandler(NewAuthHandler(config.Load(), nil, nil), nil)
	if !h.providerReady("github") {
		t.Fatal("configured GitHub login unavailable")
	}
	o, err := h.oauthConfig("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Scopes) != 0 || o.RedirectURL != "https://rss.morefreeze.top/api/auth/oauth/github/callback" || o.Endpoint.TokenURL != "https://github.com/login/oauth/access_token" {
		t.Fatal("unexpected GitHub scope or endpoints")
	}
}

func TestGitHubSubjectRejectsInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		valid  bool
	}{
		{`{"id":123456,"login":"mutable-name","email":"unverified@example.com"}`, 200, true},
		{`{"id":0}`, 200, false}, {`{"id":-1}`, 200, false}, {`{"id":"123"}`, 200, false},
		{`{"id":123.5}`, 200, false}, {`{"id":9223372036854775808}`, 200, false}, {`not json`, 200, false},
		{`{"id":123}`, 401, false}, {`{"id":123}`, 302, false},
	} {
		client := &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.github.com/user" || req.Header.Get("Authorization") != "Bearer ephemeral-token" {
				t.Fatal("incorrect identity request")
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
		})}
		sub, err := githubSubject(context.Background(), client, "ephemeral-token")
		if tc.valid {
			if err != nil || sub != "123456" {
				t.Fatal(sub, err)
			}
		} else if err == nil {
			t.Fatal("invalid identity accepted", tc.body)
		}
	}
}

func TestGitHubOAuthFlowAndBinding(t *testing.T) {
	for _, binding := range []bool{false, true} {
		t.Run(map[bool]string{false: "signup", true: "binding"}[binding], func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-jwt"}}
			cfg.Auth.PublicRegistration = true
			cfg.Auth.PublicOrigin = "https://app.example.com"
			cfg.Auth.GitHubClientID = "github-client"
			cfg.Auth.GitHubClientSecret = "github-secret"
			users := repository.NewUserRepository(db)
			repo := repository.NewAccountAuthRepository(db)
			auth := NewAuthHandler(cfg, users, nil)
			h := NewAccountAuthHandler(auth, repo)
			uid := 0
			bearer := ""
			if binding {
				var err error
				uid, err = repo.ResolveIdentity(context.Background(), "email", "reader@example.com", "reader@example.com", 0, true)
				if err != nil {
					t.Fatal(err)
				}
				bearer, err = auth.signToken(uid, "reader", false)
				if err != nil {
					t.Fatal(err)
				}
			}
			r := gin.New()
			r.POST("/start/:provider", func(c *gin.Context) {
				if binding {
					c.Set("userID", uid)
					h.BindOAuthStart(c)
				} else {
					h.OAuthStart(c)
				}
			})
			r.GET("/callback/:provider", h.OAuthCallback)
			r.POST("/exchange", h.OAuthExchange)
			w := abuseRequest(r, "/start/github", `{"return_to":"/settings#account"}`, "192.0.2.1:12", "")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response struct {
				URL string `json:"url"`
			}
			json.Unmarshal(w.Body.Bytes(), &response)
			u, _ := url.Parse(response.URL)
			q := u.Query()
			cookie := w.Result().Cookies()[0]
			if u.Host != "github.com" || q.Get("scope") != "" || q.Get("code_challenge_method") != "S256" {
				t.Fatal("unsafe authorize parameters")
			}
			var payload []byte
			db.QueryRow(`SELECT payload FROM account_auth_flows WHERE kind='state'`).Scan(&payload)
			var flow accountOAuthFlow
			json.Unmarshal(payload, &flow)
			if q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(flow.Verifier) {
				t.Fatal("missing PKCE")
			}
			h.httpClient = &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				var body string
				switch req.URL.Host + req.URL.Path {
				case "github.com/login/oauth/access_token":
					req.ParseForm()
					if req.Form.Get("code_verifier") != flow.Verifier || req.Form.Get("client_secret") != "github-secret" || req.Form.Get("redirect_uri") != cfg.Auth.PublicOrigin+"/api/auth/oauth/github/callback" {
						t.Error("invalid token exchange")
					}
					body = `{"access_token":"ephemeral-token","token_type":"bearer","scope":""}`
				case "api.github.com/user":
					body = `{"id":123456,"email":"unverified@example.com"}`
				default:
					t.Fatal("unexpected provider endpoint", req.URL.Host+req.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			cb := httptest.NewRequest("GET", "/callback/github?state="+url.QueryEscape(q.Get("state"))+"&code=provider-code", nil)
			cb.AddCookie(cookie)
			out := httptest.NewRecorder()
			r.ServeHTTP(out, cb)
			complete, _ := url.Parse(out.Header().Get("Location"))
			ticket := complete.Query().Get("ticket")
			if len(ticket) != 43 {
				t.Fatal("callback failed", out.Code, out.Header().Get("Location"))
			}
			req := httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"ticket":"`+ticket+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			if binding {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			out = httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != 200 {
				t.Fatal("exchange failed", out.Code, out.Body.String())
			}
			var gotID int
			var email string
			if err := db.QueryRow(`SELECT user_id,email FROM account_identities WHERE provider='github' AND subject='123456'`).Scan(&gotID, &email); err != nil || email != "" || (binding && gotID != uid) {
				t.Fatal("unsafe identity persistence", gotID, email, err)
			}
			var leaks int
			db.QueryRow(`SELECT count(*) FROM account_auth_flows WHERE payload::text LIKE '%ephemeral-token%' OR payload::text LIKE '%github-secret%'`).Scan(&leaks)
			if leaks != 0 {
				t.Fatal("provider credential persisted")
			}
			replay := httptest.NewRecorder()
			r.ServeHTTP(replay, cb.Clone(context.Background()))
			if !strings.Contains(replay.Header().Get("Location"), "oauth_failed") {
				t.Fatal("replayed callback accepted")
			}
		})
	}
}
