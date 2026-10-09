package explore

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// RedditBrowserBatch contains public listing metadata, never session cookies.
type RedditBrowserBatch struct {
	Subreddit  string          `json:"subreddit"`
	Period     string          `json:"period"`
	CapturedAt time.Time       `json:"captured_at"`
	Listing    json.RawMessage `json:"listing"`
}

var subredditName = regexp.MustCompile(`^[a-z0-9_]{1,21}$`)

// NormalizeSubreddit accepts a single community, excluding aggregate feeds and
// user-profile subreddits. This also bounds provider keys and endpoint paths.
func NormalizeSubreddit(name string) (string, error) {
	name = strings.ToLower(name)
	if !subredditName.MatchString(name) || strings.HasPrefix(name, "u_") {
		return "", fmt.Errorf("invalid subreddit")
	}
	switch name {
	case "all", "popular", "friends", "mod":
		return "", fmt.Errorf("aggregate feed is not a subreddit")
	}
	return name, nil
}

func (b RedditBrowserBatch) ProviderKey() (string, error) {
	name, err := NormalizeSubreddit(b.Subreddit)
	if err != nil {
		return "", err
	}
	if b.Period != "week" && b.Period != "month" {
		return "", fmt.Errorf("unsupported period")
	}
	return "reddit-" + name + "-top-" + b.Period, nil
}

func (b RedditBrowserBatch) Parse(provider Provider, now time.Time) ([]Candidate, RedditTopStats, error) {
	fail := func(msg string) ([]Candidate, RedditTopStats, error) {
		return nil, RedditTopStats{}, fmt.Errorf("%s", msg)
	}
	if _, err := b.ProviderKey(); err != nil {
		return fail(err.Error())
	}
	if b.CapturedAt.IsZero() || b.CapturedAt.Before(now.Add(-12*time.Hour)) || b.CapturedAt.After(now.Add(5*time.Minute)) {
		return fail("capture timestamp outside allowed window")
	}
	minimum, err := redditMinScore(provider.Endpoint)
	if err != nil || minimum < DefaultRedditMinScore {
		return fail("browser discovery requires min_score >= 100")
	}
	var listing struct {
		Data struct {
			Children []struct {
				Data struct {
					Subreddit string `json:"subreddit"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b.Listing, &listing); err != nil {
		return fail("invalid listing JSON")
	}
	if len(listing.Data.Children) > 100 {
		return fail("max 100 posts per listing")
	}
	for _, child := range listing.Data.Children {
		if !strings.EqualFold(child.Data.Subreddit, b.Subreddit) {
			return fail("listing subreddit mismatch")
		}
	}
	return ParseRedditTop(provider, b.Listing)
}
