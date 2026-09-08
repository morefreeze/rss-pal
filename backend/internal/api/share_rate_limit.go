package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type shareWindow struct {
	reset time.Time
	count int
}

type shareRateLimiter struct {
	mu         sync.Mutex
	clients    map[string]shareWindow
	limit      int
	window     time.Duration
	maxClients int
	now        func() time.Time
}

func newShareRateLimiterState(limit int, window time.Duration, maxClients int, now func() time.Time) *shareRateLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	if maxClients <= 0 {
		maxClients = 1
	}
	if now == nil {
		now = time.Now
	}
	return &shareRateLimiter{
		clients: make(map[string]shareWindow), limit: limit, window: window,
		maxClients: maxClients, now: now,
	}
}

func (l *shareRateLimiter) allow(clientIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if current, ok := l.clients[clientIP]; ok {
		if !now.Before(current.reset) {
			l.clients[clientIP] = shareWindow{reset: now.Add(l.window), count: 1}
			return true
		}
		if current.count >= l.limit {
			return false
		}
		current.count++
		l.clients[clientIP] = current
		return true
	}

	for ip, current := range l.clients {
		if !now.Before(current.reset) {
			delete(l.clients, ip)
		}
	}
	if len(l.clients) >= l.maxClients {
		var oldestIP string
		var oldestReset time.Time
		for ip, current := range l.clients {
			if oldestIP == "" || current.reset.Before(oldestReset) ||
				(current.reset.Equal(oldestReset) && ip < oldestIP) {
				oldestIP, oldestReset = ip, current.reset
			}
		}
		delete(l.clients, oldestIP)
	}
	l.clients[clientIP] = shareWindow{reset: now.Add(l.window), count: 1}
	return true
}

func (l *shareRateLimiter) clientCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

func NewShareRateLimiter(limit int, window time.Duration, maxClients int, now func() time.Time) gin.HandlerFunc {
	limiter := newShareRateLimiterState(limit, window, maxClients, now)
	retryAfter := int64((limiter.window + time.Second - 1) / time.Second)
	return func(c *gin.Context) {
		setPublicShareHeaders(c)
		if !limiter.allow(c.ClientIP()) {
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
