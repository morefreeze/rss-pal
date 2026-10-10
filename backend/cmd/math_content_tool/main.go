// math_content_tool is a JSON-lines helper for server-side formula audits.
// It never connects to a database or writes content. Run old/new builds against
// identical source HTML to prove that a proposed repair is converter-only.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/mmcdole/gofeed"
)

type request struct{ Op, URL, Raw, Description string }
type response struct {
	Content string `json:"content,omitempty"`
	Raw     string `json:"raw,omitempty"`
	Error   string `json:"error,omitempty"`
	Items   []item `json:"items,omitempty"`
}
type item struct{ URL, Raw, Description string }

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req request
		var out response
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			out.Error = err.Error()
		} else {
			out = run(req)
		}
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(req request) response {
	switch req.Op {
	case "fetch":
		client := httpx.NewClient(20 * time.Second)
		r, err := http.NewRequest("GET", req.URL, nil)
		if err != nil {
			return response{Error: err.Error()}
		}
		r.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSPal formula repair)")
		res, err := client.Do(r)
		if err != nil {
			return response{Error: err.Error()}
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return response{Error: fmt.Sprintf("HTTP %d", res.StatusCode)}
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
		if err != nil {
			return response{Error: err.Error()}
		}
		if len(b) > 8<<20 {
			return response{Error: "source exceeds 8 MiB"}
		}
		return response{Raw: string(b)}
	case "feed_items":
		feed, err := gofeed.NewParser().ParseString(req.Raw)
		if err != nil {
			return response{Error: err.Error()}
		}
		out := response{}
		for _, entry := range feed.Items {
			raw := entry.Content
			if strings.TrimSpace(raw) == "" {
				raw = entry.Description
			}
			out.Items = append(out.Items, item{entry.Link, raw, entry.Description})
		}
		return out
	case "subscription":
		content, _ := rss.BuildItemContent(req.Description, req.Raw, req.URL)
		return response{Content: content}
	case "fragment":
		return response{Content: rss.NormalizeFeedContent(req.Raw, req.URL)}
	case "page":
		// Resolve URLs before the standard reader entry point, matching fetchDirect.
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(req.Raw))
		if err != nil {
			return response{Error: err.Error()}
		}
		rss.ResolveURLs(doc, req.URL)
		raw, err := doc.Html()
		if err != nil {
			return response{Error: err.Error()}
		}
		content, err := rss.NewContentFetcher().FetchContentFromReader(strings.NewReader(raw))
		if err != nil {
			return response{Error: err.Error()}
		}
		return response{Content: content}
	default:
		return response{Error: "unknown operation"}
	}
}
