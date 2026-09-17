package feedcatalog

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCatalogInput = errors.New("请检查标题、公开 RSS 地址、分类和排序；不允许内部地址或凭据")
var ErrCatalogCheck = errors.New("RSS 检查失败，未上架；请查看检查结果后重试")

type FeedCatalogService struct {
	Repo   *repository.FeedCatalogRepository
	verify func(context.Context, string) error
}

func NewFeedCatalogService(repo *repository.FeedCatalogRepository, verify func(context.Context, string) error) *FeedCatalogService {
	return &FeedCatalogService{repo, verify}
}
func CatalogRSSVerifier(base string) func(context.Context, string) error {
	fetcher := rss.NewPublicFetcher(base)
	return fetcher.CheckFeed
}
func normalizeCatalogURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrCatalogInput
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return "", ErrCatalogInput
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return "", ErrCatalogInput
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return "", ErrCatalogInput
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "80" && u.Scheme == "http" || u.Port() == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	// Preserve opaque/signed query bytes; re-encoding can change the requested feed.
	return u.String(), nil
}
func (s *FeedCatalogService) Save(ctx context.Context, id, actor int, in repository.FeedCatalogInput) (repository.FeedCatalogEntry, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Category = strings.TrimSpace(in.Category)
	in.Description = strings.TrimSpace(in.Description)
	u, err := normalizeCatalogURL(in.URL)
	if err != nil || in.Title == "" || in.Category == "" || utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Category) > 80 || utf8.RuneCountInString(in.Description) > 1000 || len(u) > 2048 || in.SortOrder < 0 || in.SortOrder > 1000000 || id > 0 && in.Revision < 1 {
		return repository.FeedCatalogEntry{}, ErrCatalogInput
	}
	in.URL = u
	return s.Repo.Save(ctx, id, actor, in)
}
func (s *FeedCatalogService) Check(ctx context.Context, id, actor, revision int, publish bool) (repository.FeedCatalogEntry, error) {
	e, err := s.Repo.Get(ctx, id)
	if err != nil {
		return e, err
	}
	if revision != e.Revision {
		return e, repository.ErrCatalogConflict
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = s.verify(checkCtx, e.URL)
	failure := ""
	// Do not persist raw transport errors (proxy addresses or credentials may occur in them).
	if err != nil {
		failure = catalogCheckFailure(err)
	}
	e, writeErr := s.Repo.RecordCheck(ctx, e, actor, failure, publish)
	if writeErr != nil {
		return e, writeErr
	}
	if publish && failure != "" {
		return e, ErrCatalogCheck
	}
	return e, nil
}

func catalogCheckFailure(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "检查超时，请稍后重试"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "连接超时，请稍后重试"
	}
	text := err.Error()
	for _, code := range []string{"401", "403", "404", "410", "429", "500", "502", "503", "504"} {
		if strings.Contains(text, "RSS HTTP status "+code) {
			return "源站返回 HTTP " + code + "，请检查地址或稍后重试"
		}
	}
	switch {
	case strings.Contains(text, "exceeds 2 MiB"):
		return "RSS 响应超过 2 MiB 限制"
	case strings.Contains(text, "has no articles"):
		return "RSS 中没有文章，暂时无法推荐"
	case strings.Contains(text, "RSS parse failed"):
		return "内容不是有效的 RSS/Atom，请填写订阅地址"
	case strings.Contains(text, "x509:"):
		return "源站 TLS 证书校验失败"
	case strings.Contains(text, "blocked"), strings.Contains(text, "private"), strings.Contains(text, "not allowed"):
		return "地址或重定向不符合公网访问要求"
	default:
		return "无法连接源站，请检查地址并稍后重试"
	}
}
