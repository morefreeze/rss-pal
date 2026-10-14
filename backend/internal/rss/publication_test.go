package rss

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestExtractPublishedAt(t *testing.T) {
	for _, tc := range []struct{ name, html, url, want string }{
		{"offset", `<meta property="article:published_time" content="2023-06-23T08:00:00+08:00">`, "", "2023-06-23T00:00:00Z"},
		{"invalid meta fallback", `<meta property="article:published_time" content="bad"><script type="application/ld+json">{"@graph":[{"@type":"WebSite","datePublished":"2000-01-01"},{"@type":["BlogPosting"],"datePublished":"2023-06-23"}]}</script>`, "", "2023-06-23T00:00:00Z"},
		{"wrong article", `<script type="application/ld+json">{"@type":"BlogPosting","url":"https://example.com/other","datePublished":"2023-06-23"}</script>`, "https://example.com/current", ""},
		{"wrong article id", `<script type="application/ld+json">{"@type":"Article","@id":"https://example.com/other#article","datePublished":"2023-06-23"}</script>`, "https://example.com/current", ""},
		{"article fragment id", `<script type="application/ld+json">{"@type":"Article","@id":"#article","datePublished":"2023-06-23"}</script>`, "https://example.com/current", "2023-06-23T00:00:00Z"},
		{"recommendation", `<script type="application/ld+json">{"@type":"WebPage","itemListElement":[{"@type":"BlogPosting","datePublished":"2023-06-23"}]}</script>`, "", ""},
		{"ambiguous", `<script type="application/ld+json">[{"@type":"BlogPosting","datePublished":"2023-06-23"},{"@type":"Article","datePublished":"2022-01-01"}]</script>`, "", ""},
		{"modified only", `<meta property="article:modified_time" content="2023-06-23"><script type="application/ld+json">{"@type":"Article","dateModified":"2023-06-23"}</script>`, "", ""},
		{"explicit time", `<article><time itemprop="datePublished" datetime="2023-06-23"></time></article>`, "", "2023-06-23T00:00:00Z"},
		{"comment", `<div class="comments"><time itemprop="datePublished" datetime="2023-06-23"></time></div>`, "", ""},
		{"comment area", `<div class="comment_area"><meta itemprop="datePublished" content="2023-06-23"></div>`, "", ""},
		{"related article", `<aside><article><header><time datetime="2023-06-23"></time></header></article></aside>`, "", ""},
		{"related id", `<section id="related-posts"><time itemprop="datePublished" datetime="2023-06-23"></time></section>`, "", ""},
		{"schema comment", `<section itemtype="https://schema.org/Comment"><time itemprop="datePublished" datetime="2023-06-23"></time></section>`, "", ""},
		{"comment JSON-LD", `<div id="comments"><script type="application/ld+json">{"@type":"BlogPosting","datePublished":"2023-06-23"}</script></div>`, "", ""},
		{"case insensitive JSON-LD", `<script type="Application/LD+JSON">{"@type":"Article","datePublished":"2023-06-23"}</script>`, "", "2023-06-23T00:00:00Z"},
		{"updated header", `<article><header>Updated <time datetime="2023-06-23"></time></header></article>`, "", ""},
		{"revised header", `<article><header>Last revised <time datetime="2023-06-23"></time></header></article>`, "", ""},
		{"revised post date", `<div class="post-date">Last revised <time datetime="2023-06-23"></time></div>`, "", ""},
		{"wechat lower priority fallback", `<meta name="publish_date" content="2023-06-23T08:00:00+08:00"><meta name="rsspal:wechat_publish_time" content="2023-06-22 08:00">`, "https://mp.weixin.qq.com/s/a", "2023-06-23T00:00:00Z"},
		{"wechat invalid meta fallback", `<meta name="publish_date" content="invalid"><meta name="rsspal:wechat_publish_time" content="2023-06-22 08:00">`, "https://mp.weixin.qq.com/s/a", "2023-06-22T00:00:00Z"},
		{"wechat JSON-LD before visible fallback", `<meta name="rsspal:wechat_publish_time" content="2023-06-22 08:00"><script type="application/ld+json">{"@type":"Article","datePublished":"2023-06-23T08:00:00+08:00"}</script>`, "https://mp.weixin.qq.com/s/a", "2023-06-23T00:00:00Z"},
		{"embedded video", `<script type="application/ld+json">{"@type":"VideoObject","datePublished":"2023-06-23","embedUrl":"https://youtube.com/embed/abc"}</script>`, "https://example.com/article", ""},
		{"body date", `<article><p>A historical event on 2023-06-23.</p></article>`, "", ""},
		{"wechat", `<span id="publish_time">2023年6月23日 08:00</span>`, "https://mp.weixin.qq.com/s/a", "2023-06-23T00:00:00Z"},
		{"relative wechat", `<span id="publish_time">昨天 08:00</span>`, "https://mp.weixin.qq.com/s/a", ""},
		{"future", `<meta property="article:published_time" content="2999-06-23">`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatal(err)
			}
			got := ExtractPublishedAt(doc, tc.url)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unexpected %v", got)
				}
				return
			}
			if got == nil || got.Format(time.RFC3339) != tc.want {
				t.Fatalf("got %v want %s", got, tc.want)
			}
		})
	}
}

func TestContentMetadataKeepsPublicationBeforeCleanup(t *testing.T) {
	html := `<html><head><script type="application/ld+json">{"@type":"Article","datePublished":"2023-06-23"}</script></head><body><article><p>` + strings.Repeat("Article body. ", 30) + `</p></article></body></html>`
	result, err := NewContentFetcher().FetchContentWithMetadataFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if result.PublishedAt == nil || result.PublishedAt.Format("2006-01-02") != "2023-06-23" {
		t.Fatalf("date %v", result.PublishedAt)
	}
	if strings.Contains(result.Content, "datePublished") {
		t.Fatal("JSON-LD leaked into content")
	}
}
