package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AccountAuthHandler struct {
	auth       *AuthHandler
	repo       *repository.AccountAuthRepository
	sendEmail  func(context.Context, string, string) error
	httpClient *http.Client
	sesSender  *sesEmailSender
}

func NewAccountAuthHandler(auth *AuthHandler, repo *repository.AccountAuthRepository) *AccountAuthHandler {
	h := &AccountAuthHandler{auth: auth, repo: repo, httpClient: &http.Client{Timeout: 15 * time.Second}}
	h.sendEmail = h.smtpEmail
	if auth.cfg.Auth.EmailProvider == "tencent-ses" {
		h.sesSender = newSESEmailSender(auth.cfg.Auth)
		h.sendEmail = h.sesSender.Send
	}
	return h
}
func randomAuthToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func authHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (h *AccountAuthHandler) keyedHash(s string) string {
	m := hmac.New(sha256.New, []byte(h.auth.cfg.Auth.OTPSecret))
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}
func (h *AccountAuthHandler) origin() string {
	s := strings.TrimSuffix(h.auth.cfg.Auth.PublicOrigin, "/")
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return s
}
func (h *AccountAuthHandler) captchaReady() bool {
	a := h.auth.cfg.Auth
	switch a.CaptchaProvider {
	case "tencent":
		id, e := strconv.ParseUint(a.TencentCaptchaAppID, 10, 64)
		return e == nil && id > 0 && a.TencentCaptchaAppSecret != "" && a.TencentSecretID != "" && a.TencentSecretKey != ""
	case "", "turnstile":
		return a.TurnstileSiteKey != "" && a.TurnstileSecret != "" && len(a.TurnstileHostnames) > 0
	}
	return false
}
func (h *AccountAuthHandler) emailReady() bool {
	a := h.auth.cfg.Auth
	_, e := mail.ParseAddress(a.SMTPFrom)
	providerReady := false
	switch a.EmailProvider {
	case "", "smtp":
		providerReady = a.SMTPHost != "" && e == nil
	case "tencent-ses":
		providerReady = h.sesSender != nil && h.sesSender.ready
	}
	return h.origin() != "" && len(a.OTPSecret) >= 32 && providerReady && h.auth.registrationVerifier != nil && h.captchaReady()
}
func (h *AccountAuthHandler) Methods(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"public_registration": h.auth.cfg.Auth.PublicRegistration, "legacy_password": true, "email": h.emailReady(), "qq": h.providerReady("qq"), "github": h.providerReady("github"), "google": h.providerReady("google"), "apple": h.providerReady("apple")})
}
func (h *AccountAuthHandler) sameOrigin(c *gin.Context) bool {
	o := c.GetHeader("Origin")
	if o != "" && o != h.origin() {
		c.JSON(403, gin.H{"error": "请求来源无效"})
		return false
	}
	return true
}
func (h *AccountAuthHandler) EmailCode(c *gin.Context)     { h.emailCode(c, 0) }
func (h *AccountAuthHandler) BindEmailCode(c *gin.Context) { h.emailCode(c, getUserID(c)) }
func (h *AccountAuthHandler) emailCode(c *gin.Context, bind int) {
	if !h.sameOrigin(c) {
		return
	}
	if !h.emailReady() || h.repo == nil {
		c.JSON(503, gin.H{"error": "邮箱登录尚未配置"})
		return
	}
	var req struct {
		Email     string `json:"email"`
		Proof     string `json:"captcha_response"`
		Turnstile string `json:"cf-turnstile-response"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "邮箱地址无效"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	parsed, e := mail.ParseAddress(email)
	if e != nil || parsed.Address != email || len(email) > 254 {
		c.JSON(400, gin.H{"error": "邮箱地址无效"})
		return
	}
	if bind == 0 {
		proof := req.Proof
		if proof == "" {
			proof = req.Turnstile
		}
		if err := h.auth.registrationVerifier.Verify(c.Request.Context(), proof, c.ClientIP()); err != nil {
			status := 403
			if errors.Is(err, ErrVerificationUnavailable) {
				status = 503
			}
			c.JSON(status, gin.H{"error": "请重新完成人机验证"})
			return
		}
	} else {
		u, e := h.auth.userRepo.FindByID(bind)
		if e != nil || u == nil {
			c.JSON(401, gin.H{"error": "请重新登录"})
			return
		}
	}
	id := randomAuthToken()
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		c.JSON(503, gin.H{"error": "暂时无法发送验证码"})
		return
	}
	code := fmt.Sprintf("%06d", n.Int64())
	err = h.repo.CreateEmailChallenge(c.Request.Context(), id, email, h.keyedHash(id+":"+code), h.keyedHash("ip:"+c.ClientIP()), bind)
	if errors.Is(err, repository.ErrAccountRateLimit) {
		c.Header("Retry-After", "60")
		c.JSON(429, gin.H{"error": "发送过于频繁，请稍后重试", "retry_after": 60})
		return
	}
	if err != nil {
		c.JSON(503, gin.H{"error": "暂时无法发送验证码"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err = h.sendEmail(ctx, email, code); err != nil {
		invalidateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.repo.InvalidateEmail(invalidateCtx, id)
		c.JSON(503, gin.H{"error": "邮件发送失败，请稍后重试"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"challenge_id": id, "retry_after": 60})
}
func (h *AccountAuthHandler) EmailVerify(c *gin.Context)     { h.emailVerify(c, 0) }
func (h *AccountAuthHandler) BindEmailVerify(c *gin.Context) { h.emailVerify(c, getUserID(c)) }
func (h *AccountAuthHandler) emailVerify(c *gin.Context, bind int) {
	if !h.sameOrigin(c) {
		return
	}
	if !h.emailReady() || h.repo == nil {
		c.JSON(503, gin.H{"error": "邮箱登录尚未配置"})
		return
	}
	var req struct {
		ID       string `json:"challenge_id"`
		Code     string `json:"code"`
		Remember bool   `json:"remember"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.ID) > 128 || len(req.Code) != 6 {
		c.JSON(400, gin.H{"error": "验证码无效"})
		return
	}
	uid, e := h.repo.VerifyEmailChallenge(c.Request.Context(), req.ID, h.keyedHash(req.ID+":"+req.Code), bind, h.auth.cfg.Auth.PublicRegistration)
	if e != nil {
		c.JSON(400, gin.H{"error": "验证码无效或已过期"})
		return
	}
	if bind != 0 {
		c.JSON(200, gin.H{"bound": true})
		return
	}
	h.respondSession(c, uid, req.Remember, "")
}
func (h *AccountAuthHandler) respondSession(c *gin.Context, uid int, remember bool, ret string) {
	user, e := h.auth.userRepo.FindByID(uid)
	if e != nil || user == nil {
		c.JSON(401, gin.H{"error": "账户不可用"})
		return
	}
	token, e := h.auth.generateToken(user)
	if e != nil {
		c.JSON(503, gin.H{"error": "登录暂不可用"})
		return
	}
	resp := gin.H{"token": token, "user": user}
	if ret != "" {
		resp["return_to"] = ret
	}
	if remember && h.auth.refreshRepo != nil {
		plain, _, e := h.auth.refreshRepo.Issue(uid, refreshTokenTTL, c.GetHeader("User-Agent"))
		if e == nil {
			resp["refresh_token"] = plain
			resp["refresh_token_expires_at"] = time.Now().Add(refreshTokenTTL).UTC()
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, resp)
}
func (h *AccountAuthHandler) Identities(c *gin.Context) {
	rows, e := h.repo.Identities(c.Request.Context(), getUserID(c))
	if e != nil {
		c.JSON(503, gin.H{"error": "暂时无法读取登录方式"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"identities": rows})
}
func (h *AccountAuthHandler) smtpEmail(ctx context.Context, to, code string) error {
	a := h.auth.cfg.Auth
	port := a.SMTPPort
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(a.SMTPHost, port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	tlsCfg := &tls.Config{ServerName: a.SMTPHost, MinVersion: tls.VersionTLS12}
	if port == "465" {
		secure := tls.Client(conn, tlsCfg)
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, a.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP TLS required")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if a.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", a.SMTPUsername, a.SMTPPassword, a.SMTPHost)); err != nil {
			return err
		}
	}
	from, err := mail.ParseAddress(a.SMTPFrom)
	if err != nil {
		return err
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: RSS Pal sign-in code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour RSS Pal verification code is %s. It expires in 10 minutes.\r\n", from.String(), to, code)
	if err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
