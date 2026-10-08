package explore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const DefaultRedditMinScore = 100

// RedditTopAdapter uses Reddit's numeric score, never the position in a hot
// listing or the score text supplied by a post author.
type RedditTopAdapter struct{}

func (RedditTopAdapter) Kind() string { return "reddit_top" }
func (RedditTopAdapter) Parse(p Provider, body []byte) ([]Candidate, error) {
	candidates, _, err := ParseRedditTop(p, body)
	return candidates, err
}

type RedditTopStats struct {
	Posts            int      `json:"posts"`
	Qualified        int      `json:"qualified_posts"`
	External         int      `json:"external_posts"`
	HasMore          bool     `json:"has_more"`
	QualifiedPostIDs []string `json:"-"`
	ExternalPostIDs  []string `json:"-"`
}

// RedditTopEndpoint is shared by the read-only trial tool and the seed contract.
// The fragment configures our adapter and is never transmitted to Reddit.
func RedditTopEndpoint(subreddit, period string, minScore int) string {
	return fmt.Sprintf("https://www.reddit.com/r/%s/top.json?t=%s&limit=100&raw_json=1#min_score=%d", url.PathEscape(subreddit), url.QueryEscape(period), minScore)
}

func redditMinScore(endpoint string) (int, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse Reddit endpoint: %w", err)
	}
	if u.Fragment == "" {
		return DefaultRedditMinScore, nil
	}
	values, err := url.ParseQuery(u.Fragment)
	if err != nil || len(values) != 1 || len(values["min_score"]) != 1 {
		return 0, fmt.Errorf("Reddit endpoint requires a single positive min_score")
	}
	score, err := strconv.Atoi(values.Get("min_score"))
	if err != nil || score < 1 {
		return 0, fmt.Errorf("Reddit min_score must be a positive integer")
	}
	return score, nil
}

var redditPostID = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

// ParseRedditTop keeps the strongest qualifying post per canonical article URL.
// This bounds provenance tags even when many posts recommend the same article.
func ParseRedditTop(provider Provider, body []byte) ([]Candidate, RedditTopStats, error) {
	stats := RedditTopStats{}
	if err := checkProviderBody(body); err != nil {
		return nil, stats, err
	}
	minimum, err := redditMinScore(provider.Endpoint)
	if err != nil {
		return nil, stats, err
	}
	var listing struct {
		Kind string `json:"kind"`
		Data *struct {
			After    string `json:"after"`
			Children []struct {
				Kind string `json:"kind"`
				Data struct {
					ID          string `json:"id"`
					Score       *int   `json:"score"`
					URL         string `json:"url"`
					Destination string `json:"url_overridden_by_dest"`
					Subreddit   string `json:"subreddit"`
					Self        bool   `json:"is_self"`
					Video       bool   `json:"is_video"`
					Gallery     bool   `json:"is_gallery"`
					NSFW        bool   `json:"over_18"`
					Promoted    bool   `json:"promoted"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, stats, fmt.Errorf("parse Reddit listing: %w", err)
	}
	if listing.Kind != "Listing" || listing.Data == nil || listing.Data.Children == nil {
		return nil, stats, fmt.Errorf("invalid Reddit listing (missing children)")
	}
	stats.HasMore = listing.Data.After != ""
	byURL := make(map[string]Candidate)
	scores := make(map[string]int)
	seen := map[string]bool{}
	for _, entry := range listing.Data.Children {
		post := entry.Data
		if entry.Kind != "t3" || !redditPostID.MatchString(post.ID) || seen[post.ID] {
			continue
		}
		seen[post.ID] = true
		stats.Posts++
		if post.Score == nil || *post.Score < minimum {
			continue
		}
		stats.Qualified++
		stats.QualifiedPostIDs = append(stats.QualifiedPostIDs, post.ID)
		if post.Self || post.Video || post.Gallery || post.NSFW || post.Promoted {
			continue
		}
		target := firstNonEmpty(post.Destination, post.URL)
		target, ok := normalizePublicURL(target)
		if !ok || ignoredRedditURL(target) || ignoredRedditTopHost(hostOf(target)) {
			continue
		}
		candidate, ok := normalizeCandidate(Candidate{
			ExternalKey: "reddit:" + post.ID, FeedURL: target, Title: hostOf(target), Topic: provider.Topic,
			Tags: []string{provider.Topic, "r/" + post.Subreddit, fmt.Sprintf("score:%d", *post.Score), "https://www.reddit.com/comments/" + post.ID}, OccurrenceCount: 1,
		})
		if !ok {
			continue
		}
		stats.External++
		stats.ExternalPostIDs = append(stats.ExternalPostIDs, post.ID)
		if current, exists := byURL[target]; exists {
			count := current.OccurrenceCount + 1
			if *post.Score > scores[target] || (*post.Score == scores[target] && candidate.ExternalKey < current.ExternalKey) {
				current = candidate
				scores[target] = *post.Score
			}
			current.OccurrenceCount = count
			byURL[target] = current
		} else {
			byURL[target] = candidate
			scores[target] = *post.Score
		}
	}
	candidates := make([]Candidate, 0, len(byURL))
	for _, candidate := range byURL {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].FeedURL < candidates[j].FeedURL })
	return candidates, stats, nil
}

func ignoredRedditTopHost(host string) bool {
	for _, blocked := range []string{"youtube.com", "youtu.be", "v.redd.it", "i.redd.it", "redditmedia.com", "redditstatic.com", "imgur.com", "giphy.com", "arxiv.org", "github.com", "huggingface.co", "twitter.com", "x.com"} {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}
