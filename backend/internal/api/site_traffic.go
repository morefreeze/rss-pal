package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type SiteTrafficHandler struct {
	db     *sql.DB
	origin string
	slots  chan struct{}
	budget *shareRateLimiter
}

func NewSiteTrafficHandler(db *sql.DB, origin string) *SiteTrafficHandler {
	return &SiteTrafficHandler{
		db: db, origin: strings.TrimRight(origin, "/"), slots: make(chan struct{}, 4),
		budget: newShareRateLimiterState(300, time.Minute, 1, time.Now),
	}
}

func (h *SiteTrafficHandler) Post(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	origin := c.GetHeader("Origin")
	fetchSite := c.GetHeader("Sec-Fetch-Site")
	if !h.acceptsOrigin(origin, c.Request.Host) || (fetchSite != "" && fetchSite != "same-origin") {
		c.Status(http.StatusForbidden)
		return
	}
	if c.ContentType() != "application/json" {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	var body struct {
		Event   string `json:"event"`
		Visitor string `json:"visitor"`
		Source  string `json:"source"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 512))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF ||
		!anonymousUUID.MatchString(body.Event) || !anonymousUUID.MatchString(body.Visitor) {
		c.Status(http.StatusBadRequest)
		return
	}
	switch body.Source {
	case "direct", "internal", "external", "campaign":
	default:
		c.Status(http.StatusBadRequest)
		return
	}

	if !h.budget.allow("all") {
		c.Status(http.StatusTooManyRequests)
		return
	}

	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		c.Status(http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	_, err := h.db.ExecContext(ctx, `INSERT INTO site_page_views (event_id, visitor_id, source)
		VALUES ($1, $2, $3) ON CONFLICT (event_id) DO NOTHING`, body.Event, body.Visitor, body.Source)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SiteTrafficHandler) acceptsOrigin(origin, host string) bool {
	if h.origin != "" {
		return origin == h.origin
	}
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" && strings.EqualFold(parsed.Host, host) && origin == parsed.Scheme+"://"+parsed.Host
}

var anonymousUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
