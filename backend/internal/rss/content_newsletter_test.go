package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestBestBlogsNewsletterIncludesIntroductionAndRecommendations(t *testing.T) {
	html, err := os.ReadFile("testdata/bestblogs-newsletter.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, threshold := range []int{50, 200} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
		if err != nil {
			t.Fatal(err)
		}
		RemoveArticleChrome(doc)
		_, got := SelectArticleContent(doc, threshold)
		for _, want := range []string{"# BestBlogs.dev 第 114 期", "大家好！", "## ", "Amjad Masad", "\n\n", "适合谁", "同一夜三款模型落地", "年轻人在社会中的效用", "https://mp.weixin.qq.com/"} {
			if !strings.Contains(got, want) {
				t.Errorf("threshold %d: missing %q", threshold, want)
			}
		}
		if strings.Count(got, "\n## ") != 2 {
			t.Errorf("threshold %d: wanted both recommendation headings", threshold)
		}
		if strings.Contains(got, "Site navigation") {
			t.Error("included site navigation")
		}
	}
}

func TestBestBlogsNewsletterExtractionIsScoped(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://www.bestblogs.dev/en/newsletter/issue114", true},
		{"https://bestblogs.dev/zh/newsletter/issue114", true},
		{"https://example.com/newsletter/issue114", false},
		{"https://www.bestblogs.dev/article/114", false},
		{"", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><link rel="canonical" href="` + tc.url + `"></head><body><main><h1>Issue</h1><div class="markdown-content"><p>Introduction</p></div><article><h2>Recommendation</h2><div class="markdown-content"><p>Description</p></div></article><button>Subscribe</button></main></body></html>`))
			if err != nil {
				t.Fatal(err)
			}
			selection := bestBlogsNewsletterContent(doc)
			if (selection != nil) != tc.want {
				t.Fatalf("matched = %t, want %t", selection != nil, tc.want)
			}
			if selection != nil {
				got := ExtractMarkdown(selection)
				if strings.Count(got, "Description") != 1 || strings.Contains(got, "Subscribe") {
					t.Fatalf("duplicated description or included controls: %s", got)
				}
			}
		})
	}
}

func TestBestBlogsNewsletterHTTPExtractionRetainsResolvedLinks(t *testing.T) {
	html, err := os.ReadFile("testdata/bestblogs-newsletter.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(html) }))
	defer server.Close()
	fetcher := NewContentFetcher()
	fetcher.client = server.Client()
	got, status, err := fetcher.fetchDirect(context.Background(), server.URL+"/newsletter/issue114")
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d error=%v", status, err)
	}
	for _, want := range []string{"大家好！", "年轻人在社会中的效用", server.URL + "/article/21ef025d3a?entry=newsletter_page", "[[video:youtube:Q8yFrGwIftE]]"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q", want)
		}
	}
	if count := strings.Count(got.Content, "\n## "); count != 2 {
		t.Fatalf("recommendations = %d, want 2", count)
	}
}
