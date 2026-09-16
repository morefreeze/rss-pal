package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/opsmonitor"
	"github.com/gin-gonic/gin"
)

type AuthAttemptStore interface {
	Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error)
}

type AuthAbuseGuard struct {
	monitor *opsmonitor.Recorder
	store   AuthAttemptStore
	secret  []byte
	slots   map[string]chan struct{}
}

func NewAuthAbuseGuard(store AuthAttemptStore, secret string) *AuthAbuseGuard {
	return &AuthAbuseGuard{store: store, secret: []byte(secret), slots: map[string]chan struct{}{"login": make(chan struct{}, 8), "refresh": make(chan struct{}, 8), "register": make(chan struct{}, 4), "logout": make(chan struct{}, 4), "init": make(chan struct{}, 2)}}
}

func authClientNetwork(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "invalid"
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

func (g *AuthAbuseGuard) take(c *gin.Context, scope, value string, limit int, window time.Duration) bool {
	mac := hmac.New(sha256.New, g.secret)
	_, _ = mac.Write([]byte(scope + "\x00" + value))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	allowed, retry, err := g.store.Allow(ctx, hex.EncodeToString(mac.Sum(nil)), limit, window)
	if err != nil {
		c.Header("Retry-After", "5")
		c.AbortWithStatusJSON(503, gin.H{"error": "认证服务暂时不可用，请稍后重试"})
		return false
	}
	if !allowed {
		reason := "network"
		if strings.Contains(scope, "global") {
			reason = "global"
		} else if strings.Contains(scope, "account") || strings.Contains(scope, "token") {
			reason = "account"
		}
		at := time.Now().Add(retry)
		g.monitor.Record(opsmonitor.Event{Kind: "limit", Reason: reason, TaskType: c.GetString("monitor_action"), RetryAt: &at})
		c.Header("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
		c.AbortWithStatusJSON(429, gin.H{"error": "操作过于频繁，请稍后重试"})
		return false
	}
	return true
}

// Middleware runs before body binding, bcrypt, invite consumption and token work.
// Budgets count attempts, including failures, and do not reset on success.
func (g *AuthAbuseGuard) Middleware(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("monitor_action", action)
		if action == "register" {
			defer func() {
				reason := "failed"
				if c.Writer.Status() < 300 {
					reason = "success"
				}
				g.monitor.Record(opsmonitor.Event{Kind: "registration", Reason: reason, TaskType: "register", UserID: c.GetInt("monitor_user_id")})
			}()
		}
		c.Header("Cache-Control", "no-store")
		select {
		case g.slots[action] <- struct{}{}:
			defer func() { <-g.slots[action] }()
		default:
			g.monitor.Record(opsmonitor.Event{Kind: "limit", Reason: "global_concurrency", TaskType: action})
			c.Header("Retry-After", "5")
			c.AbortWithStatusJSON(429, gin.H{"error": "认证请求较多，请稍后重试"})
			return
		}
		ip := authClientNetwork(c.ClientIP())
		if !g.take(c, "auth-ip-"+action, ip, 120, time.Minute) {
			return
		}
		// Strictly bound even chunked bodies before decoding JSON or allocating strings.
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
		if err != nil {
			c.AbortWithStatusJSON(413, gin.H{"error": "请求内容过大"})
			return
		}
		var identity struct {
			Username     string `json:"username"`
			RefreshToken string `json:"refresh_token"`
		}
		if len(body) > 0 {
			mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || mediaType != "application/json" {
				c.AbortWithStatusJSON(415, gin.H{"error": "请使用 JSON 请求"})
				return
			}
			if err := json.Unmarshal(body, &identity); err != nil {
				c.AbortWithStatusJSON(400, gin.H{"error": "请求格式无效"})
				return
			}
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		switch action {
		case "login":
			if !g.take(c, "login-ip", ip, 30, 15*time.Minute) {
				return
			}
			if !g.take(c, "login-account", strings.ToLower(strings.TrimSpace(identity.Username)), 10, 15*time.Minute) {
				return
			}
		case "register":
			if !g.take(c, "register-ip", ip, 5, time.Hour) {
				return
			}
		case "refresh":
			if !g.take(c, "refresh-ip", ip, 60, time.Minute) || !g.take(c, "refresh-token", identity.RefreshToken, 10, time.Minute) {
				return
			}
		case "init":
			if !g.take(c, "init-ip", ip, 5, time.Hour) {
				return
			}
		}
		if !g.take(c, "auth-global-"+action, "all", 600, time.Minute) {
			return
		}
		c.Next()
	}
}

// AdmitRegistration charges the hourly enrollment budget only after a valid
// human proof; invalid proofs cannot drain it. Ordinary auth stays independent.
func (g *AuthAbuseGuard) AdmitRegistration(c *gin.Context) bool {
	return g.take(c, "register-verified-global", "all", 60, time.Hour)
}

func (g *AuthAbuseGuard) SetMonitor(m *opsmonitor.Recorder) { g.monitor = m }
