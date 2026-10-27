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

func TestQQLoginConfiguration(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_ORIGIN", "https://rss.morefreeze.top")
	t.Setenv("QQ_CLIENT_ID", "123456")
	t.Setenv("QQ_CLIENT_SECRET", "fixture-secret")
	h := NewAccountAuthHandler(NewAuthHandler(config.Load(), nil, nil), nil)
	if !h.providerReady("qq") {
		t.Fatal("configured QQ login unavailable")
	}
	o, e := h.oauthConfig("qq")
	if e != nil {
		t.Fatal(e)
	}
	if o.Endpoint.AuthURL != "https://graph.qq.com/oauth2.0/authorize" || len(o.Scopes) != 1 || o.Scopes[0] != "get_user_info" || o.RedirectURL != "https://rss.morefreeze.top/api/auth/oauth/qq/callback" {
		t.Fatal("unexpected QQ auth configuration")
	}
}

func TestQQSubjectRejectsInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, token, identity string
		status                int
		valid                 bool
	}{
		{"valid", `{"access_token":"ephemeral"}`, `{"client_id":"123456","openid":"opaque-QQ-ID"}`, 200, true},
		{"app_mismatch", `{"access_token":"ephemeral"}`, `{"client_id":"other-app","openid":"opaque-QQ-ID"}`, 200, false},
		{"missing_id", `{"access_token":"ephemeral"}`, `{"client_id":"123456"}`, 200, false},
		{"token_error", `{"error":100021,"access_token":"ephemeral"}`, `{}`, 200, false},
		{"missing_token", `{}`, `{}`, 200, false},
		{"identity_error", `{"access_token":"ephemeral"}`, `{"error":100000,"client_id":"123456","openid":"ID"}`, 200, false},
		{"invalid_json", `callback({});`, `{}`, 200, false},
		{"control_id", `{"access_token":"ephemeral"}`, `{"client_id":"123456","openid":"bad id"}`, 200, false},
		{"non_string", `{"access_token":"ephemeral"}`, `{"client_id":"123456","openid":123}`, 200, false},
		{"oversize", strings.Repeat("x", 65537), `{}`, 200, false},
		{"status", `{"access_token":"ephemeral"}`, `{}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" || req.URL.Host != "graph.qq.com" || req.URL.Query().Get("fmt") != "json" {
					t.Fatal("incorrect QQ request")
				}
				body := tc.token
				if req.URL.Path == "/oauth2.0/me" {
					body = tc.identity
					if req.URL.Query().Get("access_token") != "ephemeral" {
						t.Fatal("wrong token")
					}
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			sub, err := qqSubject(context.Background(), client, &oauth2.Config{ClientID: "123456", ClientSecret: "secret", RedirectURL: "https://app.example.com/callback"}, "code")
			if tc.valid {
				if err != nil || sub != "123456:opaque-QQ-ID" {
					t.Fatal(sub, err)
				}
			} else if err == nil {
				t.Fatal("invalid identity accepted")
			} else if strings.Contains(err.Error(), "ephemeral") || strings.Contains(err.Error(), "secret") {
				t.Fatal("credential leaked")
			}
		})
	}
}
func TestQQOAuthFlowAndBinding(t *testing.T) {
	for _, binding := range []bool{false, true} {
		t.Run(map[bool]string{false: "signup", true: "binding"}[binding], func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-jwt"}}
			cfg.Auth.PublicRegistration = true
			cfg.Auth.PublicOrigin = "https://app.example.com"
			cfg.Auth.QQClientID = "123456"
			cfg.Auth.QQClientSecret = "qq-secret"
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
			startReq := httptest.NewRequest("POST", "/start/qq", strings.NewReader(`{"return_to":"/settings#account"}`))
			startReq.Header.Set("Content-Type", "application/json")
			if binding {
				startReq.Header.Set("User-Agent", "Mozilla/5.0 Mobile")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, startReq)
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
			if (binding && q.Get("display") != "mobile") || (!binding && q.Get("display") != "") {
				t.Fatal("incorrect mobile presentation")
			}
			if u.Host != "graph.qq.com" || q.Get("scope") != "get_user_info" || q.Get("response_mode") != "" || q.Get("nonce") != "" {
				t.Fatal("unsafe authorize parameters")
			}
			h.httpClient = &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				var body string
				switch req.URL.Host + req.URL.Path {
				case "graph.qq.com/oauth2.0/token":
					req.Form = req.URL.Query()
					if req.Method != "GET" || req.Form.Get("client_secret") != "qq-secret" || req.Form.Get("redirect_uri") != cfg.Auth.PublicOrigin+"/api/auth/oauth/qq/callback" {
						t.Error("invalid token exchange")
					}
					body = `{"access_token":"ephemeral-token"}`
				case "graph.qq.com/oauth2.0/me":
					body = `{"client_id":"123456","openid":"opaque-QQ-ID"}`
				default:
					t.Fatal("unexpected provider endpoint", req.URL.Host+req.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			cb := httptest.NewRequest("GET", "/callback/qq?state="+url.QueryEscape(q.Get("state"))+"&code=provider-code", nil)
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
			if err := db.QueryRow(`SELECT user_id,email FROM account_identities WHERE provider='qq' AND subject='123456:opaque-QQ-ID'`).Scan(&gotID, &email); err != nil || email != "" || (binding && gotID != uid) {
				t.Fatal("unsafe identity persistence", gotID, email, err)
			}
			var leaks int
			db.QueryRow(`SELECT count(*) FROM account_auth_flows WHERE payload::text LIKE '%ephemeral-token%' OR payload::text LIKE '%qq-secret%'`).Scan(&leaks)
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
