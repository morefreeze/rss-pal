package rss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var ErrHackerNewsUnavailable = errors.New("HN 帖子不存在、已删除或不是帖子")

// HackerNewsItemURL recognizes the exact public item endpoint. Other HN pages
// remain ordinary URLs. Invalid item IDs are rejected by the dedicated reader.
func HackerNewsItemURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host != "news.ycombinator.com" || u.User != nil || u.Path != "/item" {
		return "", false
	}
	id, err := strconv.ParseInt(u.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 || len(u.Query()["id"]) != 1 {
		return "", true
	}
	return fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id), true
}

type hnItem struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Text        string  `json:"text"`
	By          string  `json:"by"`
	Time        int64   `json:"time"`
	Score       int     `json:"score"`
	Descendants int     `json:"descendants"`
	URL         string  `json:"url"`
	Kids        []int64 `json:"kids"`
	Deleted     bool    `json:"deleted"`
	Dead        bool    `json:"dead"`
}

func (f *ContentFetcher) fetchHNItem(ctx context.Context, id int64) (*hnItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id), nil)
	if err != nil {
		return nil, err
	}
	res, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HN API HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("HN API response too large")
	}
	var item *hnItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, err
	}
	if item == nil || item.ID != id {
		return nil, fmt.Errorf("%w（ID %d）", ErrHackerNewsUnavailable, id)
	}
	return item, nil
}

func hnMarkdown(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + raw + "</div>"))
	if err != nil {
		return ""
	}
	doc.Find("script, style, iframe, form").Remove()
	// API text can contain relative HN links.
	base, _ := url.Parse("https://news.ycombinator.com/")
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if u, err := url.Parse(href); err == nil && !u.IsAbs() {
			s.SetAttr("href", base.ResolveReference(u).String())
		}
	})
	return ExtractMarkdown(doc.Find("body"))
}

func hnLabel(raw string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "\n", " ", "\r", " ", "#", "\\#").Replace(raw)
}

func (f *ContentFetcher) fetchHackerNews(ctx context.Context, canonical string) (ContentResult, error) {
	if canonical == "" {
		return ContentResult{}, fmt.Errorf("无效的 HN 帖子 ID")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	u, _ := url.Parse(canonical)
	id, _ := strconv.ParseInt(u.Query().Get("id"), 10, 64)
	story, err := f.fetchHNItem(ctx, id)
	if err != nil {
		return ContentResult{}, fmt.Errorf("抓取 HN 帖子失败: %w", err)
	}
	if story.Deleted || story.Dead || story.Type != "story" || story.Title == "" {
		return ContentResult{}, ErrHackerNewsUnavailable
	}

	// Fetch breadth first, preserving the ranked kids order for rendering. Limits
	// bound requests as well as output; no links from comments are fetched.
	nodes := map[int64]*hnItem{}
	seen := map[int64]bool{id: true}
	pending := story.Kids
	if len(pending) > 20 {
		pending = pending[:20]
	}
	for depth := 0; depth < 3 && len(pending) > 0; depth++ {
		batch := []int64{}
		for _, cid := range pending {
			if !seen[cid] && len(seen) <= 120 {
				seen[cid] = true
				batch = append(batch, cid)
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		var firstErr error
		sem := make(chan struct{}, 6)
		for _, cid := range batch {
			wg.Add(1)
			go func(cid int64) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					mu.Lock()
					firstErr = ctx.Err()
					mu.Unlock()
					return
				}
				defer func() { <-sem }()
				node, e := f.fetchHNItem(ctx, cid)
				mu.Lock()
				defer mu.Unlock()
				if e != nil {
					if firstErr == nil {
						firstErr = e
					}
					return
				}
				if node.Type != "comment" && !node.Deleted {
					firstErr = fmt.Errorf("HN item %d is not a comment", cid)
					return
				}
				nodes[cid] = node
			}(cid)
		}
		wg.Wait()
		if firstErr != nil {
			return ContentResult{}, fmt.Errorf("抓取 HN 讨论失败: %w", firstErr)
		}
		pending = nil
		for _, cid := range batch {
			if n := nodes[cid]; n != nil {
				kids := n.Kids
				if len(kids) > 10 {
					kids = kids[:10]
				}
				pending = append(pending, kids...)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n作者：%s · 帖子得分：%d · 评论：%d\n\n[HN 原帖](%s)\n\n", hnLabel(story.Title), hnLabel(story.By), story.Score, story.Descendants, canonical)
	if story.Time > 0 {
		fmt.Fprintf(&b, "发布于：%s\n\n", time.Unix(story.Time, 0).UTC().Format("2006-01-02 15:04 UTC"))
	}
	if source, e := url.Parse(story.URL); e == nil && (source.Scheme == "https" || source.Scheme == "http") && source.Host != "" {
		fmt.Fprintf(&b, "[原文链接](%s)\n\n", source.String())
	}
	if story.Text != "" {
		b.WriteString(limitContent(hnMarkdown(story.Text), 32000) + "\n\n")
	}
	b.WriteString("## 热门讨论\n\n按 HN 原生排名选取，评论分数不公开。最多 20 个讨论分支、3 层回复。\n\n")
	var render func(int64, int, map[int64]bool)
	render = func(cid int64, depth int, path map[int64]bool) {
		n := nodes[cid]
		if n == nil || path[cid] {
			return
		}
		path[cid] = true
		defer delete(path, cid)
		var block strings.Builder
		if !n.Deleted && !n.Dead {
			fmt.Fprintf(&block, "### %s · [原评论](https://news.ycombinator.com/item?id=%d)", hnLabel(n.By), cid)
			if n.Time > 0 {
				fmt.Fprintf(&block, " · %s", time.Unix(n.Time, 0).UTC().Format("2006-01-02 15:04 UTC"))
			}
			body := hnMarkdown(n.Text)
			if len(body) > 16000 {
				body = limitContent(body, 16000) + "\n\n（长评论已截断，请查看原评论）"
			}
			block.WriteString("\n\n" + body + "\n\n")
		} else {
			block.WriteString("（评论已删除或不可见）\n\n")
		}
		prefix := strings.Repeat("> ", depth)
		for _, line := range strings.Split(strings.TrimSpace(block.String()), "\n") {
			b.WriteString(prefix + line + "\n")
		}
		b.WriteString("\n")
		shown := 0
		if depth < 2 {
			for _, kid := range n.Kids {
				if nodes[kid] != nil && !path[kid] {
					render(kid, depth+1, path)
					shown++
				}
			}
		}
		if hidden := len(n.Kids) - shown; hidden > 0 {
			fmt.Fprintf(&b, "%s[%d 条回复未展开](https://news.ycombinator.com/item?id=%d)\n\n", prefix, hidden, cid)
		}
	}
	shown := 0
	for _, cid := range story.Kids {
		if nodes[cid] != nil {
			render(cid, 0, map[int64]bool{})
			shown++
		}
	}
	if len(story.Kids) > shown {
		fmt.Fprintf(&b, "[%d 个讨论分支未展开](%s)\n", len(story.Kids)-shown, canonical)
	}
	if story.Descendants == 0 && len(story.Kids) == 0 {
		b.WriteString("暂无评论。\n")
	}
	return ContentResult{Title: story.Title, Content: b.String()}, nil
}
