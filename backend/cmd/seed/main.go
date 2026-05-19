package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
)

type seedFeed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Language    string `json:"language"`
	FeedType    string `json:"feed_type"`
	SortOrder   int    `json:"sort_order"`
}

// 12 sources, curated from bestblogs.dev Issue #93.
// RSSHub-routed URLs use the canonical internal hostname http://rsshub:1200.
// When probing from the host machine, set RSSHUB_BASE=http://localhost:1200
// (after temporarily exposing the port in docker-compose.yml) so probe HTTP
// requests are reachable. The canonical URL is always stored in the DB.
var seeds = []seedFeed{
	{URL: "https://openai.com/news/rss.xml", Title: "OpenAI Blog", Description: "OpenAI 官方博客", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 1},
	{URL: "https://www.anthropic.com/news/feed.xml", Title: "Anthropic News", Description: "Anthropic / Claude 官方更新", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 2},
	{URL: "https://blog.cloudflare.com/rss/", Title: "Cloudflare Blog", Description: "Cloudflare 工程博客", Category: "enterprise", Language: "en", FeedType: "rss", SortOrder: 1},
	{URL: "https://feed.infoq.com/", Title: "InfoQ", Description: "Software architecture and engineering", Category: "enterprise", Language: "en", FeedType: "rss", SortOrder: 2},
	{URL: "https://baoyu.io/feed.xml", Title: "宝玉的分享", Description: "宝玉的 AI / 工程笔记", Category: "ai_eng", Language: "zh", FeedType: "rss", SortOrder: 10},
	{URL: "https://www.qbitai.com/feed", Title: "量子位", Description: "AI 行业新闻与解读", Category: "cn_tech", Language: "zh", FeedType: "rss", SortOrder: 1},
	{URL: "https://www.youtube.com/feeds/videos.xml?user=SequoiaCapital", Title: "Sequoia Capital (YouTube)", Description: "红杉资本访谈与分享", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 20},
	{URL: "https://www.youtube.com/feeds/videos.xml?user=ycombinator", Title: "Y Combinator (YouTube)", Description: "YC 创业者访谈", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 21},
	{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UCFzCkTM2OmkSHcrZftJa9-w", Title: "AI Engineer (YouTube)", Description: "AI Engineer summit & talks", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 22},
	{URL: "https://addyo.substack.com/feed", Title: "Addy Osmani", Description: "Software engineering essays", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 30},
	{URL: "http://rsshub:1200/wechat/ce/MzI5MDA1MDU4MA==", Title: "腾讯技术工程", Description: "腾讯技术工程 公众号(via RSSHub)", Category: "cn_tech", Language: "zh", FeedType: "rss", SortOrder: 50},
	{URL: "http://rsshub:1200/xiaoyuzhou/podcast/6388ea5cb164f9c40c2cdb40", Title: "小宇宙播客", Description: "中文 AI / 工程主题播客(via RSSHub)", Category: "podcast", Language: "zh", FeedType: "podcast", SortOrder: 60},
}

// probeURL returns the URL to actually probe. If RSSHUB_BASE is set, replace
// the canonical rsshub:1200 prefix so probes succeed from the host machine.
// The canonical URL is always what gets stored in the database.
func probeURL(canonicalURL string) string {
	base := os.Getenv("RSSHUB_BASE")
	if base != "" {
		return strings.Replace(canonicalURL, "http://rsshub:1200", base, 1)
	}
	return canonicalURL
}

type report struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	HTTPStatus int    `json:"http_status"`
	ProbeError string `json:"probe_error,omitempty"`
	IsBroken   bool   `json:"is_broken"`
	WroteFeeds bool   `json:"wrote_feeds"`
}

func probe(url string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "rss-pal-seed/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func main() {
	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	reports := make([]report, 0, len(seeds))

	for _, s := range seeds {
		r := report{URL: s.URL, Title: s.Title}

		// Probe via potentially-overridden URL, but store canonical URL.
		pURL := probeURL(s.URL)
		status, err := probe(pURL)
		r.HTTPStatus = status
		if err != nil {
			r.ProbeError = err.Error()
			r.IsBroken = true
		} else if status < 200 || status >= 400 {
			r.IsBroken = true
		}

		// If healthy, seed into feeds (shared, owner_id=NULL).
		if !r.IsBroken {
			_, errF := db.Exec(`
				INSERT INTO feeds (url, title, fetch_interval_minutes, owner_id, feed_type, is_active)
				VALUES ($1, $2, 60, NULL, $3, true)
				ON CONFLICT (url) DO NOTHING
			`, s.URL, s.Title, s.FeedType)
			if errF != nil {
				log.Printf("feeds insert failed for %s: %v", s.URL, errF)
			} else {
				r.WroteFeeds = true
			}
		}

		reports = append(reports, r)
		log.Printf("[%s] status=%d broken=%v feed=%v",
			s.URL, r.HTTPStatus, r.IsBroken, r.WroteFeeds)
	}

	// Print JSON report at the end so it's easy to grep.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(reports)

	// Sanity: count what we wrote.
	var ok, broken int
	for _, r := range reports {
		if r.IsBroken {
			broken++
		} else if r.WroteFeeds {
			ok++
		}
	}
	log.Printf("seed complete: %d healthy feeds written, %d broken (skipped)", ok, broken)
}
