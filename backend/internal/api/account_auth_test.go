package api

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAccountMethodsFailClosed(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.PublicRegistration = true
	h := NewAuthHandler(cfg, nil, nil)
	p := NewAccountAuthHandler(h, nil)
	r := gin.New()
	r.GET("/methods", p.Methods)
	r.POST("/register", h.Register)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/methods", nil))
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	if m["email"] != false || m["google"] != false || m["apple"] != false || m["github"] != false {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"a","password":"abcdef","code":"invite"}`)))
	if w.Code != 403 {
		t.Fatalf("unverified signup bypass %d", w.Code)
	}
}
func TestEmailLoginDeliveryAndBinding(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test"}}
	cfg.Auth.PublicRegistration = true
	cfg.Auth.PublicOrigin = "https://example.com"
	cfg.Auth.OTPSecret = strings.Repeat("x", 32)
	cfg.Auth.SMTPHost = "smtp.example.com"
	cfg.Auth.SMTPFrom = "a@example.com"
	h := NewAuthHandler(cfg, repository.NewUserRepository(db), nil)
	cfg.Auth.TurnstileSiteKey = "site"
	cfg.Auth.TurnstileSecret = "secret"
	cfg.Auth.TurnstileHostnames = []string{"example.com"}
	h.registrationVerifier = &proofVerifier{}
	p := NewAccountAuthHandler(h, repository.NewAccountAuthRepository(db))
	var code string
	p.sendEmail = func(_ context.Context, email, c string) error { code = c; return nil }
	r := gin.New()
	r.POST("/code", p.EmailCode)
	r.POST("/verify", p.EmailVerify)
	w := abuseRequest(r, "/code", `{"email":"reader@example.com","captcha_response":"fresh-proof"}`, "192.0.2.1:12", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var resp struct {
		Challenge string `json:"challenge_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(code) != 6 {
		t.Fatal("no six-digit code")
	}
	body := `{"challenge_id":"` + resp.Challenge + `","code":"` + code + `","remember":false}`
	w = abuseRequest(r, "/verify", body, "192.0.2.1:12", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = abuseRequest(r, "/verify", body, "192.0.2.1:12", "")
	if w.Code != 400 {
		t.Fatal("replayed code accepted")
	}
}

func TestEmailFailedDeliveryInvalidatesCode(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	cfg := &config.Config{}
	cfg.Auth.PublicRegistration = true
	cfg.Auth.PublicOrigin = "https://example.com"
	cfg.Auth.OTPSecret = strings.Repeat("k", 32)
	cfg.Auth.SMTPHost = "smtp.example.com"
	cfg.Auth.SMTPFrom = "a@example.com"
	h := NewAuthHandler(cfg, repository.NewUserRepository(db), nil)
	cfg.Auth.TurnstileSiteKey = "site"
	cfg.Auth.TurnstileSecret = "secret"
	cfg.Auth.TurnstileHostnames = []string{"example.com"}
	h.registrationVerifier = &proofVerifier{}
	p := NewAccountAuthHandler(h, repository.NewAccountAuthRepository(db))
	p.sendEmail = func(context.Context, string, string) error { return context.DeadlineExceeded }
	r := gin.New()
	r.POST("/code", p.EmailCode)
	w := abuseRequest(r, "/code", `{"email":"delivery@example.com","captcha_response":"fresh-proof"}`, "192.0.2.1:12", "")
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	var consumed bool
	if err := db.QueryRow(`SELECT consumed FROM account_email_challenges`).Scan(&consumed); err != nil || !consumed {
		t.Fatalf("failed delivery usable: %v %v", consumed, err)
	}
}
func TestOAuthCompletionRequiresBrowserAndSameOwner(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret"}}
	cfg.Auth.PublicOrigin = "https://example.com"
	cfg.Auth.PublicRegistration = true
	ur := repository.NewUserRepository(db)
	a, e := ur.CreateAdmin("owner-a", "password")
	if e != nil {
		t.Fatal(e)
	}
	b, e := ur.CreateAdmin("owner-b", "password")
	if e != nil {
		t.Fatal(e)
	}
	h := NewAuthHandler(cfg, ur, nil)
	repo := repository.NewAccountAuthRepository(db)
	p := NewAccountAuthHandler(h, repo)
	router := gin.New()
	router.POST("/exchange", p.OAuthExchange)
	browser := randomAuthToken()
	ticket := randomAuthToken()
	f := accountOAuthFlow{Provider: "google", Subject: "external-one", BindUserID: a.ID, ReturnTo: "/settings"}
	if e := repo.PutFlow(context.Background(), authHash(ticket), "completion", authHash(browser), f); e != nil {
		t.Fatal(e)
	}
	token, _ := h.generateToken(b)
	req := httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"ticket":"`+ticket+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: authBrowserCookie, Value: browser})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatalf("switched account accepted: %d %s", w.Code, w.Body.String())
	}
	rows, _ := repo.Identities(context.Background(), a.ID)
	if len(rows) != 0 {
		t.Fatal("identity bound before checking active owner")
	}
	// Wrong browser cannot consume a ticket. Original browser can complete it once.
	ticket = randomAuthToken()
	f.BindUserID = 0
	if e := repo.PutFlow(context.Background(), authHash(ticket), "completion", authHash(browser), f); e != nil {
		t.Fatal(e)
	}
	request := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"ticket":"`+ticket+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: authBrowserCookie, Value: cookie})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := request(randomAuthToken()); w.Code != 400 {
		t.Fatal("wrong browser accepted")
	}
	if w := request(browser); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(browser); w.Code != 400 {
		t.Fatal("completion replay accepted")
	}
}
func TestSafeAuthReturns(t *testing.T) {
	for _, s := range []string{"//evil.test", "https://evil.test", "/\\evil.test", "/admin", "/auth/complete", "/%2f%2fevil.test"} {
		if safeAuthReturn(s) != "/articles" {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"/capture?url=https%3A%2F%2Fexample.com", "/feeds?add=1&source=a", "/settings"} {
		if safeAuthReturn(s) != s {
			t.Fatal(s)
		}
	}
}
func TestEmailReadinessRequiresConfiguredHumanVerification(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.PublicOrigin = "https://example.com"
	cfg.Auth.OTPSecret = strings.Repeat("s", 32)
	cfg.Auth.SMTPHost = "smtp.example.com"
	cfg.Auth.SMTPFrom = "sender@example.com"
	h := NewAccountAuthHandler(NewAuthHandler(cfg, nil, nil), nil)
	if h.emailReady() {
		t.Fatal("email advertised with unconfigured human verification")
	}
}

func TestEmailBindingBusinessErrorsPreserveAuthenticatedSession(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "session-preserved"}}
	cfg.Auth.PublicOrigin = "https://example.com"
	cfg.Auth.OTPSecret = strings.Repeat("s", 32)
	cfg.Auth.SMTPHost = "smtp.example.com"
	cfg.Auth.SMTPFrom = "sender@example.com"
	cfg.Auth.TurnstileSiteKey = "site"
	cfg.Auth.TurnstileSecret = "secret"
	cfg.Auth.TurnstileHostnames = []string{"example.com"}
	users := repository.NewUserRepository(db)
	owner, err := users.CreateAdmin("binding-owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	auth := NewAuthHandler(cfg, users, nil)
	repo := repository.NewAccountAuthRepository(db)
	h := NewAccountAuthHandler(auth, repo)
	r := gin.New()
	r.Use(auth.AuthMiddleware())
	r.POST("/binding", h.BindEmailVerify)
	r.GET("/me", auth.GetMe)
	token, err := auth.generateToken(owner)
	if err != nil {
		t.Fatal(err)
	}
	id := randomAuthToken()
	if err = repo.CreateEmailChallenge(context.Background(), id, "bound@example.com", h.keyedHash(id+":123456"), "ip", owner.ID); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, code := range []string{"bad", "000000"} {
		w := request("POST", "/binding", `{"challenge_id":"`+id+`","code":"`+code+`"}`)
		if w.Code != 400 {
			t.Fatalf("business error uses session-invalidating status %d: %s", w.Code, w.Body.String())
		}
	}
	var attempts int
	if err = db.QueryRow(`SELECT attempts FROM account_email_challenges WHERE id=$1`, id).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("wrong code must spend one guess: %d %v", attempts, err)
	}
	if _, err = db.Exec(`UPDATE account_email_challenges SET expires_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/binding", `{"challenge_id":"`+id+`","code":"123456"}`); w.Code != 400 {
		t.Fatalf("expired binding status %d", w.Code)
	}
	if w := request("GET", "/me", ""); w.Code != 200 {
		t.Fatalf("valid session changed: %d %s", w.Code, w.Body.String())
	}
}
