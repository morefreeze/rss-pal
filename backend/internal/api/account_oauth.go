package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/subtle"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authBrowserCookie = "__Host-rsspal_auth_browser"

type accountOAuthFlow struct {
	QQNickname  string `json:"qq_nickname,omitempty"`
	QQAvatarURL string `json:"qq_avatar_url,omitempty"`
	Provider    string `json:"provider"`
	Nonce       string `json:"nonce"`
	Verifier    string `json:"verifier,omitempty"`
	BindUserID  int    `json:"bind_user_id"`
	Remember    bool   `json:"remember"`
	ReturnTo    string `json:"return_to"`
	Subject     string `json:"subject,omitempty"`
	Email       string `json:"email,omitempty"`
}

func (h *AccountAuthHandler) appleKey() (*ecdsa.PrivateKey, error) {
	k, e := jwt.ParseECPrivateKeyFromPEM([]byte(strings.ReplaceAll(h.auth.cfg.Auth.ApplePrivateKey, `\n`, "\n")))
	if e != nil {
		return nil, e
	}
	if k.Curve != elliptic.P256() {
		return nil, errors.New("Apple requires P-256")
	}
	return k, nil
}
func (h *AccountAuthHandler) providerReady(p string) bool {
	a := h.auth.cfg.Auth
	if h.origin() == "" {
		return false
	}
	switch p {
	case "qq":
		return a.QQClientID != "" && a.QQClientSecret != ""
	case "github":
		return a.GitHubClientID != "" && a.GitHubClientSecret != ""
	case "google":
		return a.GoogleClientID != "" && a.GoogleClientSecret != ""
	case "apple":
		_, e := h.appleKey()
		return a.AppleClientID != "" && a.AppleTeamID != "" && a.AppleKeyID != "" && e == nil
	}
	return false
}
func safeAuthReturn(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.IsAbs() || u.Host != "" || u.User != nil || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return "/articles"
	}
	switch u.Path {
	case "/articles", "/feeds", "/capture", "/settings", "/explore":
		return u.String()
	}
	return "/articles"
}
func (h *AccountAuthHandler) browser(c *gin.Context, create bool) string {
	s, e := c.Cookie(authBrowserCookie)
	if e == nil && len(s) == 43 {
		return s
	}
	if !create {
		return ""
	}
	s = randomAuthToken()
	http.SetCookie(c.Writer, &http.Cookie{Name: authBrowserCookie, Value: s, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: 1200})
	return s
}
func (h *AccountAuthHandler) oauthConfig(provider string) (*oauth2.Config, error) {
	a := h.auth.cfg.Auth
	o := &oauth2.Config{RedirectURL: h.origin() + "/api/auth/oauth/" + provider + "/callback"}
	switch provider {
	case "qq":
		o.ClientID = a.QQClientID
		o.ClientSecret = a.QQClientSecret
		o.Scopes = []string{"get_user_info"}
		o.Endpoint = oauth2.Endpoint{AuthURL: "https://graph.qq.com/oauth2.0/authorize", TokenURL: "https://graph.qq.com/oauth2.0/token", AuthStyle: oauth2.AuthStyleInParams}
	case "github":
		o.ClientID = a.GitHubClientID
		o.ClientSecret = a.GitHubClientSecret
		o.Endpoint = oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}
	case "google":
		o.ClientID = a.GoogleClientID
		o.ClientSecret = a.GoogleClientSecret
		o.Scopes = []string{"openid", "email", "profile"}
		o.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}
	case "apple":
		k, e := h.appleKey()
		if e != nil {
			return nil, e
		}
		now := time.Now()
		t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{Issuer: a.AppleTeamID, Subject: a.AppleClientID, Audience: jwt.ClaimStrings{"https://appleid.apple.com"}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute))})
		t.Header["kid"] = a.AppleKeyID
		secret, e := t.SignedString(k)
		if e != nil {
			return nil, e
		}
		o.ClientID = a.AppleClientID
		o.ClientSecret = secret
		o.Scopes = []string{"name", "email"}
		o.Endpoint = oauth2.Endpoint{AuthURL: "https://appleid.apple.com/auth/authorize", TokenURL: "https://appleid.apple.com/auth/token", AuthStyle: oauth2.AuthStyleInParams}
	default:
		return nil, errors.New("unsupported provider")
	}
	return o, nil
}
func (h *AccountAuthHandler) OAuthStart(c *gin.Context)     { h.oauthStart(c, 0) }
func (h *AccountAuthHandler) BindOAuthStart(c *gin.Context) { h.oauthStart(c, getUserID(c)) }
func (h *AccountAuthHandler) oauthStart(c *gin.Context, bind int) {
	if !h.sameOrigin(c) {
		return
	}
	provider := c.Param("provider")
	if !h.providerReady(provider) || h.repo == nil {
		c.JSON(503, gin.H{"error": "该登录方式尚未配置"})
		return
	}
	if bind != 0 {
		u, e := h.auth.userRepo.FindByID(bind)
		if e != nil || u == nil {
			c.JSON(401, gin.H{"error": "请重新登录"})
			return
		}
	}
	var req struct {
		Remember bool   `json:"remember"`
		ReturnTo string `json:"return_to"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "请求无效"})
		return
	}
	o, e := h.oauthConfig(provider)
	if e != nil {
		c.JSON(503, gin.H{"error": "该登录方式尚未配置"})
		return
	}
	state := randomAuthToken()
	f := accountOAuthFlow{Provider: provider, Nonce: randomAuthToken(), BindUserID: bind, Remember: req.Remember, ReturnTo: safeAuthReturn(req.ReturnTo)}
	opts := []oauth2.AuthCodeOption{}
	if provider != "qq" {
		opts = append(opts, oauth2.SetAuthURLParam("nonce", f.Nonce))
	}
	if provider == "qq" && (strings.Contains(c.Request.UserAgent(), "Mobile") || strings.Contains(c.Request.UserAgent(), "Android")) {
		opts = append(opts, oauth2.SetAuthURLParam("display", "mobile"))
	}
	if provider == "google" || provider == "github" {
		f.Verifier = oauth2.GenerateVerifier()
		opts = append(opts, oauth2.S256ChallengeOption(f.Verifier))
	} else if provider == "apple" {
		opts = append(opts, oauth2.SetAuthURLParam("response_mode", "form_post"))
	}
	if e = h.repo.PutFlow(c.Request.Context(), authHash(state), "state", authHash(h.browser(c, true)), f); e != nil {
		c.JSON(503, gin.H{"error": "登录暂不可用"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"url": o.AuthCodeURL(state, opts...)})
}
func (h *AccountAuthHandler) OAuthCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	fail := func() { c.Redirect(303, h.origin()+"/auth/complete?error=oauth_failed") }
	provider := c.Param("provider")
	if !h.providerReady(provider) || h.repo == nil {
		c.JSON(503, gin.H{"error": "该登录方式尚未配置"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if c.Request.ParseForm() != nil {
		fail()
		return
	}
	browser := h.browser(c, false)
	state := c.Request.Form.Get("state")
	if browser == "" || len(state) > 128 {
		fail()
		return
	}
	var f accountOAuthFlow
	if h.repo.TakeFlow(c.Request.Context(), authHash(state), "state", authHash(browser), &f) != nil || f.Provider != provider {
		fail()
		return
	}
	code := c.Request.Form.Get("code")
	if code == "" || len(code) > 8192 || c.Request.Form.Get("error") != "" {
		fail()
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	client := h.httpClient
	if provider == "github" || provider == "qq" {
		copy := *client
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	o, e := h.oauthConfig(provider)
	if e != nil {
		fail()
		return
	}
	opts := []oauth2.AuthCodeOption{}
	if provider == "google" || provider == "github" {
		opts = append(opts, oauth2.VerifierOption(f.Verifier))
	}
	var subject, email string
	if provider == "qq" {
		profile, profileErr := qqAccount(ctx, client, o, code)
		subject, e = profile.Subject, profileErr
		f.QQNickname, f.QQAvatarURL = profile.Nickname, profile.AvatarURL
		if e != nil {
			fail()
			return
		}
	} else {
		tok, e := o.Exchange(ctx, code, opts...)
		if e != nil {
			fail()
			return
		}
		if provider == "github" {
			subject, e = githubSubject(ctx, client, tok.AccessToken)
		} else {
			raw, ok := tok.Extra("id_token").(string)
			if !ok {
				fail()
				return
			}
			issuer, jwks := "https://accounts.google.com", "https://www.googleapis.com/oauth2/v3/certs"
			if provider == "apple" {
				issuer, jwks = "https://appleid.apple.com", "https://appleid.apple.com/auth/keys"
			}
			verifier := oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(ctx, jwks), &oidc.Config{ClientID: o.ClientID, SupportedSigningAlgs: []string{oidc.RS256}})
			subject, email, e = verifyOAuthClaims(ctx, verifier, raw, f.Nonce)
		}
		if e != nil {
			fail()
			return
		}
	}
	f.Subject = subject
	f.Email = email
	f.Nonce = ""
	f.Verifier = ""
	ticket := randomAuthToken()
	if h.repo.PutFlow(ctx, authHash(ticket), "completion", authHash(browser), f) != nil {
		fail()
		return
	}
	c.Redirect(303, h.origin()+"/auth/complete?ticket="+url.QueryEscape(ticket))
}

// Binding happens only after proving the same active application account again.
func (h *AccountAuthHandler) requestUser(c *gin.Context) (int, error) {
	s := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if s == "" {
		return 0, nil
	}
	claims := &Claims{}
	tok, e := jwt.ParseWithClaims(s, claims, func(t *jwt.Token) (any, error) { return []byte(h.auth.cfg.JWT.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}))
	if e != nil || !tok.Valid {
		return 0, errors.New("invalid session")
	}
	u, e := h.auth.userRepo.FindByID(claims.UserID)
	if e != nil || u == nil {
		return 0, errors.New("invalid session")
	}
	return u.ID, nil
}
func (h *AccountAuthHandler) OAuthExchange(c *gin.Context) {
	if !h.sameOrigin(c) {
		return
	}
	c.Header("Cache-Control", "no-store")
	if h.repo == nil {
		c.JSON(503, gin.H{"error": "登录暂不可用"})
		return
	}
	var req struct {
		Ticket string `json:"ticket"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Ticket) != 43 {
		c.JSON(400, gin.H{"error": "登录凭证无效"})
		return
	}
	browser := h.browser(c, false)
	if browser == "" {
		c.JSON(400, gin.H{"error": "请在原浏览器重新登录"})
		return
	}
	var f accountOAuthFlow
	if h.repo.TakeFlow(c.Request.Context(), authHash(req.Ticket), "completion", authHash(browser), &f) != nil {
		c.JSON(400, gin.H{"error": "登录凭证无效或已过期"})
		return
	}
	uid, e := h.requestUser(c)
	if e != nil || (f.BindUserID != 0 && uid != f.BindUserID) || (f.BindUserID == 0 && uid != 0) {
		c.JSON(409, gin.H{"error": "当前账户已变化，请重新开始登录或绑定"})
		return
	}
	var id int
	if f.Provider == "qq" {
		id, e = h.repo.ResolveQQIdentity(c.Request.Context(), f.Subject, f.BindUserID, h.auth.cfg.Auth.PublicRegistration, f.QQNickname, f.QQAvatarURL)
	} else {
		id, e = h.repo.ResolveIdentity(c.Request.Context(), f.Provider, f.Subject, f.Email, f.BindUserID, h.auth.cfg.Auth.PublicRegistration)
	}
	if e != nil {
		c.JSON(409, gin.H{"error": "该登录方式已绑定其他账户，或注册尚未开放"})
		return
	}
	if f.BindUserID != 0 {
		c.JSON(200, gin.H{"bound": true, "return_to": f.ReturnTo})
		return
	}
	h.respondSession(c, id, f.Remember, f.ReturnTo)
}

// The library validates signature, fixed issuer, audience and expiry; bind nonce
// explicitly because the OIDC verifier intentionally leaves that to its caller.
func verifyOAuthClaims(ctx context.Context, v *oidc.IDTokenVerifier, raw, nonce string) (string, string, error) {
	id, e := v.Verify(ctx, raw)
	if e != nil {
		return "", "", e
	}
	if id.Subject == "" || len(id.Subject) > 512 || nonce == "" || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		return "", "", errors.New("invalid identity claims")
	}
	var claims struct {
		Email    string `json:"email"`
		Verified any    `json:"email_verified"`
	}
	if e = id.Claims(&claims); e != nil {
		return "", "", e
	}
	email := ""
	if claims.Verified == true || claims.Verified == "true" {
		email = claims.Email
	}
	return id.Subject, email, nil
}
