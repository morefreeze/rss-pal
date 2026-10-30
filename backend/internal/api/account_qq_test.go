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

func TestQQReviewMethods(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, pending := range []bool{false, true} {
			cfg := config.Load()
			cfg.Auth.PublicOrigin = "https://rss.morefreeze.top"
			cfg.Auth.QQClientID = "123456"
			cfg.Auth.QQClientSecret = ""
			if configured {
				cfg.Auth.QQClientSecret = "fixture-secret"
			}
			cfg.Auth.QQReviewPending = pending
			h := NewAccountAuthHandler(NewAuthHandler(cfg, nil, nil), nil)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			h.Methods(c)
			var result map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["qq"] != configured || result["qq_review_pending"] != (configured && pending) {
				t.Fatalf("configured=%v pending=%v: unexpected methods", configured, pending)
			}
		}
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
				if req.URL.Path == "/user/get_user_info" {
					if req.URL.Query().Get("access_token") != "ephemeral" || req.URL.Query().Get("oauth_consumer_key") != "123456" || req.URL.Query().Get("openid") != "opaque-QQ-ID" {
						t.Fatal("invalid user info parameters")
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":0,"nickname":"QQ读者","figureurl_qq_1":"https://q.qlogo.cn/qqapp/123456/id/40"}`)), Request: req}, nil
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
			sub, err := qqAccount(context.Background(), client, &oauth2.Config{ClientID: "123456", ClientSecret: "secret", RedirectURL: "https://app.example.com/callback"}, "code")
			if tc.valid {
				if err != nil || (sub.Subject != "123456:opaque-QQ-ID" || sub.Nickname != "QQ读者" || sub.AvatarURL == "") {
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
				if _, err = db.Exec(`UPDATE users SET nickname='原昵称',is_admin=true WHERE id=$1`, uid); err != nil {
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
				case "graph.qq.com/user/get_user_info":
					if req.Method != "GET" || req.URL.Query().Get("oauth_consumer_key") != "123456" || req.URL.Query().Get("openid") != "opaque-QQ-ID" || req.URL.Query().Get("access_token") != "ephemeral-token" {
						t.Fatal("invalid profile request")
					}
					// A restricted provider nickname must not block signup/binding or
					// enter the transient completion payload before the repository.
					body = `{"ret":0,"nickname":"ＡＤＭＩＮ_01","figureurl_qq_2":"http://thirdqq.qlogo.cn/qqapp/123456/id/100","gender":"private-ignored"}`
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
			var pendingLeaks int
			if err := db.QueryRow(`SELECT count(*) FROM account_auth_flows WHERE payload::text LIKE '%ephemeral-token%' OR payload::text LIKE '%qq-secret%' OR payload::text LIKE '%private-ignored%' OR payload::text LIKE '%ＡＤＭＩＮ_01%'`).Scan(&pendingLeaks); err != nil || pendingLeaks != 0 {
				t.Fatal("provider credentials or extra profile fields persisted before exchange", err)
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
			var display, avatar string
			if err := db.QueryRow(`SELECT display_name,avatar_url FROM account_identities WHERE user_id=$1 AND provider='qq'`, gotID).Scan(&display, &avatar); err != nil || display != "QQ读者" || avatar != "https://thirdqq.qlogo.cn/qqapp/123456/id/100" {
				t.Fatal("profile not persisted", err)
			}
			stored, err := users.FindByID(gotID)
			if err != nil || stored.QQNickname != "QQ读者" || stored.QQAvatarURL != avatar {
				t.Fatal("profile not exposed", err)
			}
			if binding && (stored.Nickname != "原昵称" || !stored.IsAdmin) {
				t.Fatal("existing user changed")
			}
			if !binding && stored.NeedsNickname {
				t.Fatal("QQ profile should be the initial display fallback")
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

func TestQQUserInfoFailures(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"denied", `{"ret":1002,"msg":"token-should-not-leak"}`},
		{"missing_ret", `{"nickname":"QQ"}`},
		{"partial", `{"ret":0,"is_lost":1,"nickname":"QQ"}`},
		{"control_name", `{"ret":0,"nickname":"bad\nname"}`},
		{"too_long_name", `{"ret":0,"nickname":"` + strings.Repeat("名", 129) + `"}`},
		{"bad_json", `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				body := `{"access_token":"ephemeral"}`
				if req.URL.Path == "/oauth2.0/me" {
					body = `{"client_id":"123456","openid":"id"}`
				}
				if req.URL.Path == "/user/get_user_info" {
					body = tc.body
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			_, err := qqAccount(context.Background(), client, &oauth2.Config{ClientID: "123456", ClientSecret: "secret", RedirectURL: "https://app.example/callback"}, "code")
			if err == nil || strings.Contains(err.Error(), "token-should-not-leak") {
				t.Fatal("invalid profile accepted or leaked")
			}
		})
	}
}

func TestQQAvatarURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"http://thirdqq.qlogo.cn/qqapp/id/100?access_token=discard#fragment", "https://thirdqq.qlogo.cn/qqapp/id/100"},
		{"javascript:alert(1)", ""}, {"https://qlogo.cn.attacker.example/p", ""},
		{"https://user:pass@q.qlogo.cn/p", ""}, {"https://q.qlogo.cn:8443/p", ""},
	} {
		if got := qqAvatarURL(tc.input); got != tc.want {
			t.Fatalf("avatar validation failed: %q", got)
		}
	}
	if got := qqAvatarURL("https://evil.example/100", "https://q.qlogo.cn/qqapp/id/40"); got != "https://q.qlogo.cn/qqapp/id/40" {
		t.Fatal("40px fallback missing")
	}
}

func TestQQRestrictedNicknameUsesSafeDefault(t *testing.T) {
	for _, name := range []string{"admin", "ＡＤＭＩＮ_01", "管 理 員", "f.u.c.k", "裸聊加微信"} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: authFixtureTransport(func(req *http.Request) (*http.Response, error) {
				body := `{"access_token":"ephemeral"}`
				if req.URL.Path == "/oauth2.0/me" {
					body = `{"client_id":"123456","openid":"id"}`
				}
				if req.URL.Path == "/user/get_user_info" {
					data, _ := json.Marshal(map[string]any{"ret": 0, "nickname": name, "figureurl_qq_1": "https://q.qlogo.cn/qqapp/id/40"})
					body = string(data)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			profile, err := qqAccount(context.Background(), client, &oauth2.Config{ClientID: "123456", ClientSecret: "secret", RedirectURL: "https://app.example/callback"}, "code")
			if err != nil || profile.Subject != "123456:id" || profile.Nickname != "QQ读者" || profile.AvatarURL == "" {
				t.Fatalf("nickname must fall back without blocking verified QQ identity: %+v %v", profile, err)
			}
		})
	}
}
