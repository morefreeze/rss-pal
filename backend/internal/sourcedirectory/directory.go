// Package sourcedirectory exposes the complete public RSSHub/Folo route catalog.
package sourcedirectory

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const SourceURL = "https://docs.rsshub.app/routes.json"
const maxBytes int64 = 20 << 20
const ttl = 24 * time.Hour

//go:embed snapshot.json
var snapshot []byte

type Directory struct {
	SourceURL     string     `json:"source_url"`
	FetchedAt     string     `json:"fetched_at"`
	Stale         bool       `json:"stale"`
	PlatformCount int        `json:"platform_count"`
	RouteCount    int        `json:"route_count"`
	Platforms     []Platform `json:"platforms"`
}
type Platform struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Categories []string `json:"categories"`
	Heat       float64  `json:"heat"`
	Routes     []Route  `json:"routes"`
}
type Route struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Path           string               `json:"path"`
	Paths          []string             `json:"paths"`
	Categories     []string             `json:"categories"`
	Description    string               `json:"description"`
	Example        string               `json:"example"`
	Parameters     map[string]Parameter `json:"parameters"`
	RequiresConfig bool                 `json:"requires_config"`
	ConfigNames    []string             `json:"config_names"`
	AntiCrawler    bool                 `json:"anti_crawler"`
	TopFeeds       []TopFeed            `json:"top_feeds"`
}
type Parameter struct {
	Description string   `json:"description"`
	Default     string   `json:"default,omitempty"`
	Options     []Option `json:"options,omitempty"`
}
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type TopFeed struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type upstreamPlatform struct {
	Name       string                   `json:"name"`
	URL        string                   `json:"url"`
	Categories []string                 `json:"categories"`
	Heat       float64                  `json:"heat"`
	Routes     map[string]upstreamRoute `json:"routes"`
}
type upstreamFeed struct {
	Title       string          `json:"title"`
	URL         string          `json:"url"`
	OwnerUserID json.RawMessage `json:"ownerUserId"`
}

type upstreamRoute struct {
	Name        string                     `json:"name"`
	Path        json.RawMessage            `json:"path"`
	Categories  []string                   `json:"categories"`
	Description string                     `json:"description"`
	Example     string                     `json:"example"`
	Parameters  map[string]json.RawMessage `json:"parameters"`
	Features    struct {
		RequireConfig json.RawMessage `json:"requireConfig"`
		AntiCrawler   bool            `json:"antiCrawler"`
	} `json:"features"`
	TopFeeds []upstreamFeed `json:"topFeeds"`
}

// Normalize uses an explicit field whitelist; upstream feed ownership, diagnostics,
// images, HTML and extra fields can never become response fields.
func Normalize(data []byte, fetchedAt time.Time) (Directory, error) {
	var upstream map[string]upstreamPlatform
	if err := json.Unmarshal(data, &upstream); err != nil {
		return Directory{}, fmt.Errorf("invalid directory: %w", err)
	}
	if len(upstream) == 0 {
		return Directory{}, errors.New("empty directory")
	}
	d := Directory{SourceURL: SourceURL, FetchedAt: fetchedAt.UTC().Format(time.RFC3339), Platforms: make([]Platform, 0, len(upstream))}
	for namespace, p := range upstream {
		if namespace == "" || p.Routes == nil {
			return Directory{}, errors.New("missing platform routes")
		}
		out := Platform{ID: namespace, Name: p.Name, URL: safeURL(p.URL, false), Heat: p.Heat, Categories: []string{}, Routes: make([]Route, 0, len(p.Routes))}
		for key, r := range p.Routes {
			paths := []string{}
			var path string
			if json.Unmarshal(r.Path, &path) == nil {
				paths = append(paths, path)
			} else if json.Unmarshal(r.Path, &paths) != nil || len(paths) == 0 {
				return Directory{}, errors.New("invalid route paths")
			}
			categories := unique(r.Categories)
			if len(categories) == 0 {
				categories = unique(p.Categories)
			}
			route := Route{ID: namespace + ":" + key, Name: r.Name, Path: paths[0], Paths: paths, Categories: categories, Description: r.Description, Example: safeExample(r.Example), Parameters: map[string]Parameter{}, ConfigNames: []string{}, TopFeeds: []TopFeed{}, AntiCrawler: r.Features.AntiCrawler}
			// Route descriptions are plain JSON strings, never interpreted as HTML.
			for name, raw := range r.Parameters {
				var desc string
				parameter := Parameter{}
				if json.Unmarshal(raw, &desc) == nil {
					parameter.Description = desc
				} else {
					var obj struct {
						Description string `json:"description"`
						Default     any    `json:"default"`
						Options     []struct {
							Label any `json:"label"`
							Value any `json:"value"`
						} `json:"options"`
					}
					if json.Unmarshal(raw, &obj) == nil {
						parameter.Description = obj.Description
						parameter.Default = scalar(obj.Default)
						for _, o := range obj.Options {
							value := scalar(o.Value)
							if value != "" {
								label := scalar(o.Label)
								if label == "" {
									label = value
								}
								parameter.Options = append(parameter.Options, Option{label, value})
							}
						}
					}
				}
				route.Parameters[name] = parameter
			}
			var required bool
			if json.Unmarshal(r.Features.RequireConfig, &required) == nil {
				route.RequiresConfig = required
			} else {
				var configs []struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(r.Features.RequireConfig, &configs) == nil {
					route.RequiresConfig = len(configs) > 0
					for _, c := range configs {
						if c.Name != "" {
							route.ConfigNames = append(route.ConfigNames, c.Name)
						}
					}
					route.ConfigNames = unique(route.ConfigNames)
				}
			}
			for _, f := range r.TopFeeds {
				if len(f.OwnerUserID) > 0 && string(f.OwnerUserID) != "null" {
					continue
				}
				if u := safeURL(f.URL, true); u != "" {
					route.TopFeeds = append(route.TopFeeds, TopFeed{Title: f.Title, URL: u})
				}
			}
			out.Routes = append(out.Routes, route)
			out.Categories = append(out.Categories, categories...)
		}
		if len(out.Routes) == 0 {
			out.Categories = p.Categories
		}
		out.Categories = unique(out.Categories)
		sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].ID < out.Routes[j].ID })
		d.RouteCount += len(out.Routes)
		d.Platforms = append(d.Platforms, out)
	}
	if d.RouteCount == 0 {
		return Directory{}, errors.New("directory has no routes")
	}
	sort.Slice(d.Platforms, func(i, j int) bool {
		if d.Platforms[i].Heat != d.Platforms[j].Heat {
			return d.Platforms[i].Heat > d.Platforms[j].Heat
		}
		return d.Platforms[i].ID < d.Platforms[j].ID
	})
	d.PlatformCount = len(d.Platforms)
	return d, nil
}
func scalar(v any) string {
	switch v.(type) {
	case string, bool, float64:
		return fmt.Sprint(v)
	default:
		return ""
	}
}
func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	sort.Strings(result)
	return result
}

func safeExample(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		if safeURL("https://rsshub.app"+raw, false) != "" {
			return raw
		}
		return ""
	}
	return safeURL(raw, true)
}

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func safeURL(raw string, allowRSSHub bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || u.Opaque != "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if u.Scheme == "rsshub" {
		if !allowRSSHub || !namespacePattern.MatchString(host) || u.Port() != "" || u.Path == "" {
			return ""
		}
	} else if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") {
		return ""
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return ""
		}
	} else if u.Scheme != "rsshub" && (!strings.Contains(host, ".") || net.ParseIP(host) != nil) {
		return ""
	}
	for key := range u.Query() {
		lower := strings.ToLower(key)
		for _, sensitive := range []string{"token", "secret", "password", "passwd", "cookie", "credential", "authorization", "api_key", "apikey", "access_key", "auth"} {
			if strings.Contains(lower, sensitive) {
				return ""
			}
		}
	}
	return u.String()
}

func embedded() (Directory, error) {
	var d Directory
	err := json.Unmarshal(snapshot, &d)
	if err == nil && (d.PlatformCount == 0 || d.RouteCount == 0) {
		err = errors.New("empty embedded directory")
	}
	return d, err
}

type Service struct {
	mu          sync.Mutex
	directory   Directory
	client      *http.Client
	refreshing  bool
	nextAttempt time.Time
	failed      bool
}

func New() *Service {
	d, err := embedded()
	if err != nil {
		panic("invalid embedded source directory: " + err.Error())
	}
	return &Service{directory: d, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Get always serves the immutable in-memory snapshot immediately. The first
// request after expiry initiates one bounded background refresh; failures retain
// the previous snapshot and retry only after five minutes, with no scheduler.
func (s *Service) Get() Directory {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	fetched, err := time.Parse(time.RFC3339, s.directory.FetchedAt)
	expired := err != nil || now.Sub(fetched) >= ttl
	d := s.directory
	d.Stale = expired || s.failed
	if expired && !s.refreshing && !now.Before(s.nextAttempt) {
		s.refreshing = true
		s.nextAttempt = now.Add(5 * time.Minute)
		go func() { err := s.refresh(); s.mu.Lock(); s.failed = err != nil; s.refreshing = false; s.mu.Unlock() }()
	}
	return d
}
func (s *Service) refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("directory status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return errors.New("directory exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return errors.New("directory exceeds size limit")
	}
	d, err := Normalize(data, time.Now())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.directory = d
	s.failed = false
	s.mu.Unlock()
	return nil
}
