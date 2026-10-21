package repository

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// ExploreDiscoveryChannel describes provenance, independently of content topic.
// Keys are stable across Reddit listing windows and repeated observations.
type ExploreDiscoveryChannel struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

var redditDiscoveryKey = regexp.MustCompile(`^reddit-([a-z0-9_]+)-top-(week|month)$`)
var discoveryFilterKey = regexp.MustCompile(`^[a-z0-9_:-]{1,150}$`)

func ValidExploreDiscoveryFilter(key string) bool {
	return key == "" || discoveryFilterKey.MatchString(key)
}

func discoveryChannel(kind, key string) ExploreDiscoveryChannel {
	if kind == "reddit_top" || kind == "reddit_stream" {
		sub := strings.TrimPrefix(key, "reddit-")
		if match := redditDiscoveryKey.FindStringSubmatch(key); match != nil {
			sub = match[1]
		}
		return ExploreDiscoveryChannel{Key: "reddit:" + sub, Kind: "reddit", Label: "Reddit · r/" + sub}
	}
	names := map[string]string{
		"chinese-independent": "中文独立博客", "plenary-programming-opml": "Plenary 编程",
		"plenary-tech-opml": "Plenary 科技", "plenary-webdev-opml": "Plenary Web 开发",
		"ooh-recently-added": "ooh.directory 新近收录", "awesome-selfhosted": "Awesome Selfhosted",
	}
	name := names[key]
	if name == "" {
		name = key
	}
	prefix := map[string]string{"opml": "目录", "directory": "目录", "github_awesome": "GitHub 清单", "related_site": "关联站点"}[kind]
	if prefix == "" {
		prefix = kind
	}
	label := prefix + " · " + name
	if kind == "related_site" {
		label = "关联站点"
	}
	return ExploreDiscoveryChannel{Key: kind + ":" + key, Kind: kind, Label: label}
}

// Include disabled providers: removing collection does not erase provenance.
func enrichExploreDiscovery(db Querier, articles []ExploreArticleListItem) error {
	ids := []int{}
	seen := map[int]bool{}
	for _, a := range articles {
		if !seen[a.SourceID] {
			ids = append(ids, a.SourceID)
			seen[a.SourceID] = true
		}
	}
	channels := map[int]map[string]ExploreDiscoveryChannel{}
	if len(ids) > 0 {
		rows, err := db.Query(`SELECT DISTINCT o.source_id,p.provider_kind,p.provider_key FROM explore_source_observations o JOIN explore_registry_providers p ON p.id=o.provider_id WHERE o.source_id=ANY($1)`, pq.Array(ids))
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int
			var kind, key string
			if err := rows.Scan(&id, &kind, &key); err != nil {
				rows.Close()
				return err
			}
			c := discoveryChannel(kind, key)
			if channels[id] == nil {
				channels[id] = map[string]ExploreDiscoveryChannel{}
			}
			channels[id][c.Key] = c
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for i := range articles {
		articles[i].DiscoveryChannels = sortedDiscoveryChannels(channels[articles[i].SourceID])
	}
	return nil
}

func sortedDiscoveryChannels(values map[string]ExploreDiscoveryChannel) []ExploreDiscoveryChannel {
	result := make([]ExploreDiscoveryChannel, 0, len(values))
	for _, c := range values {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func readExploreDiscoveryChoices(db Querier) ([]ExploreDiscoveryChannel, error) {
	rows, err := db.Query(`SELECT provider_kind,provider_key FROM explore_registry_providers WHERE enabled OR EXISTS (SELECT 1 FROM explore_source_observations o WHERE o.provider_id=explore_registry_providers.id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	choices := map[string]ExploreDiscoveryChannel{}
	groups := map[string]string{"reddit": "Reddit", "opml": "OPML 目录", "directory": "网站目录", "github_awesome": "GitHub 清单", "related_site": "关联站点"}
	for rows.Next() {
		var kind, key string
		if err := rows.Scan(&kind, &key); err != nil {
			return nil, err
		}
		c := discoveryChannel(kind, key)
		if c.Kind != "related_site" {
			choices[c.Key] = c
		}
		choices[c.Kind] = ExploreDiscoveryChannel{Key: c.Kind, Kind: c.Kind, Label: groups[c.Kind]}
	}
	return sortedDiscoveryChannels(choices), rows.Err()
}

func filterExploreDiscovery(articles []ExploreArticleListItem, channel string) []ExploreArticleListItem {
	if channel == "" {
		return articles
	}
	result := make([]ExploreArticleListItem, 0, len(articles))
	for _, a := range articles {
		for _, c := range a.DiscoveryChannels {
			if c.Key == channel || c.Kind == channel {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

// Saved articles are paginated by SQL, so provenance filtering must happen there.
func exploreDiscoverySQLFilter(source, parameter string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM explore_source_observations discovery JOIN explore_registry_providers provider ON provider.id=discovery.provider_id WHERE discovery.source_id=%s AND (
 CASE WHEN provider.provider_kind IN ('reddit_top','reddit_stream') THEN 'reddit' ELSE provider.provider_kind END = %s OR
 CASE WHEN provider.provider_kind='reddit_top' THEN 'reddit:' || regexp_replace(provider.provider_key,'^reddit-(.*)-top-(week|month)$','\1')
 WHEN provider.provider_kind='reddit_stream' THEN 'reddit:' || regexp_replace(provider.provider_key,'^reddit-','')
 ELSE provider.provider_kind || ':' || provider.provider_key END = %s))`, source, parameter, parameter)
}
