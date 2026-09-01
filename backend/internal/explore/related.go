package explore

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"golang.org/x/net/html"
)

const (
	MaxRelatedSeeds         = 200
	RelatedPriorityDirect   = 400
	RelatedPriorityIndirect = 100
)

type RelatedSiteSyncStore interface {
	LoadRelatedSeeds(context.Context, time.Time, int) ([]string, error)
	LoadRelatedProvider(time.Time) (*RegistryProvider, error)
	UpsertCandidate(providerID int, candidate Candidate, observedAt time.Time) (int, error)
	RecordSuccess(providerID int, syncedAt time.Time, etag, lastModified string) error
	RecordFailure(providerID int, syncedAt time.Time, cause error) error
}

type RelatedSiteSync struct {
	Store  RelatedSiteSyncStore
	Queue  RegistryQueue
	Client ProviderClient
}

type RelatedSiteSyncResult struct {
	Seeds      int
	Candidates int
	Failures   int
	Err        error
}

// Sync discovers from a bounded projection of owner-visible formal URLs. The
// persisted observation carries only generic related-site provenance.
func (syncer RelatedSiteSync) Sync(ctx context.Context, now time.Time) RelatedSiteSyncResult {
	result := RelatedSiteSyncResult{}
	if syncer.Store == nil || syncer.Queue == nil {
		result.Err = errors.New("related site sync store and queue are required")
		return result
	}
	provider, err := syncer.Store.LoadRelatedProvider(now)
	if err != nil || provider == nil {
		result.Err = err
		return result
	}
	seeds, err := syncer.Store.LoadRelatedSeeds(ctx, now, MaxRelatedSeeds)
	if err != nil {
		result.Err = errors.Join(err, syncer.Store.RecordFailure(provider.ID, now, err))
		return result
	}
	result.Seeds = len(seeds)
	var syncErr error
	successfulSeeds := 0
	type aggregatedCandidate struct {
		candidate Candidate
		priority  int
	}
	aggregated := make(map[string]aggregatedCandidate)
	for _, seed := range seeds {
		fetched, fetchErr := syncer.Client.Fetch(ctx, seed, "", "")
		if fetchErr != nil {
			result.Failures++
			syncErr = errors.Join(syncErr, fetchErr)
			continue
		}
		candidates, discoverErr := (RelatedSiteDiscoverer{}).Discover(seed, fetched.Body)
		if discoverErr != nil {
			result.Failures++
			syncErr = errors.Join(syncErr, discoverErr)
			continue
		}
		successfulSeeds++
		for _, candidate := range NormalizeCandidates(candidates) {
			priority := RelatedPriorityIndirect
			key := "indirect\x00" + candidate.ExternalKey
			if candidate.SiteURL != "" && candidate.SiteURL != candidate.FeedURL {
				priority = RelatedPriorityDirect
				key = "direct\x00" + candidate.FeedURL
			}
			current, exists := aggregated[key]
			if !exists || candidate.FeedURL < current.candidate.FeedURL {
				candidate.OccurrenceCount += current.candidate.OccurrenceCount
				current = aggregatedCandidate{candidate: candidate, priority: priority}
			} else {
				current.candidate.OccurrenceCount += candidate.OccurrenceCount
			}
			aggregated[key] = current
		}
	}
	keys := make([]string, 0, len(aggregated))
	for key := range aggregated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := aggregated[key]
		sourceID, upsertErr := syncer.Store.UpsertCandidate(provider.ID, item.candidate, now)
		if upsertErr != nil {
			result.Failures++
			syncErr = errors.Join(syncErr, upsertErr)
			continue
		}
		if queueErr := syncer.Queue.Enqueue(sourceID, model.ExploreFetchTaskValidateSource, item.priority); queueErr != nil {
			result.Failures++
			syncErr = errors.Join(syncErr, queueErr)
			continue
		}
		result.Candidates++
	}
	if successfulSeeds > 0 || len(seeds) == 0 {
		result.Err = errors.Join(syncErr, syncer.Store.RecordSuccess(provider.ID, now, "", ""))
		return result
	}
	result.Err = errors.Join(syncErr, syncer.Store.RecordFailure(provider.ID, now, syncErr))
	return result
}

// RelatedSiteDiscoverer finds public feeds and a small, bounded set of sites
// linked from an already-fetched public page. It does not fetch or validate.
type RelatedSiteDiscoverer struct{}

func (RelatedSiteDiscoverer) Discover(pageURL string, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}
	base, ok := normalizePublicURL(pageURL)
	if !ok {
		return nil, nil
	}
	baseURL, _ := url.Parse(base)
	baseHost := hostOf(base)
	declared := newCandidateCollector(20, func(candidate Candidate) string { return candidate.FeedURL })
	external := newCandidateCollector(10, func(candidate Candidate) string { return candidate.ExternalKey })
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		switch strings.ToLower(token.Data) {
		case "link":
			if !isFeedDeclaration(token) {
				continue
			}
			href, found := htmlAttribute(token, "href")
			if !found {
				continue
			}
			if resolved, ok := resolvePublicURL(baseURL, href); ok {
				declared.add(Candidate{ExternalKey: resolved, FeedURL: resolved, SiteURL: base, Title: baseHost, OccurrenceCount: 1})
			}
		case "a":
			href, found := htmlAttribute(token, "href")
			if !found {
				continue
			}
			resolved, ok := resolvePublicURL(baseURL, href)
			if !ok || hostOf(resolved) == baseHost || ignoredRelatedURL(resolved) {
				continue
			}
			domain := hostOf(resolved)
			external.add(Candidate{ExternalKey: domain, FeedURL: resolved, SiteURL: resolved, Title: domain, OccurrenceCount: 1})
		}
	}
	return append(declared.candidates(), external.candidates()...), nil
}

func isFeedDeclaration(token html.Token) bool {
	rel, hasRel := htmlAttribute(token, "rel")
	typeValue, hasType := htmlAttribute(token, "type")
	if !hasRel || !hasType || !hasASCIIToken(rel, "alternate") {
		return false
	}
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	return typeValue == "application/rss+xml" || typeValue == "application/atom+xml"
}

func hasASCIIToken(value, want string) bool {
	for start := 0; start < len(value); {
		for start < len(value) && isASCIIWhitespace(value[start]) {
			start++
		}
		end := start
		for end < len(value) && !isASCIIWhitespace(value[end]) {
			end++
		}
		if start < end && strings.EqualFold(value[start:end], want) {
			return true
		}
		start = end
	}
	return false
}

func isASCIIWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func htmlAttribute(token html.Token, name string) (string, bool) {
	for _, attr := range token.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func resolvePublicURL(base *url.URL, raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return normalizePublicURL(base.ResolveReference(u).String())
}

func ignoredRelatedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	path := strings.ToLower(u.Path)
	for _, suffix := range []string{".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".mp4", ".webm", ".mp3", ".woff", ".woff2"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return isPrivateHost(hostOf(raw))
}
