package rss

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestContentFetcherPreservesCompleteLongDirectArticles(t *testing.T) {
	for _, paragraph := range []string{
		"The complete English article preserves every paragraph and its final references.",
		"完整的中文正文必须保留每一个段落以及结尾的代码、图片和参考链接。",
	} {
		t.Run(paragraph[:3], func(t *testing.T) {
			const count = 900
			raw := `<html><head><title>Complete article</title></head><body><article>` +
				strings.Repeat("<p>"+paragraph+"</p>", count) +
				`<pre><code class="language-go">func finalExample() {
    println("complete code")
}</code></pre><p><a href="/references/final">Final reference</a></p>` +
				`<img src="/figures/final.png" alt="Final figure"><p>COMPLETE ARTICLE SUFFIX 正文结束</p></article></body></html>`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, raw)
			}))
			defer server.Close()
			fetcher := &ContentFetcher{client: server.Client()}
			got, err := fetcher.FetchContentWithMetadata(context.Background(), server.URL+"/article")
			if err != nil {
				t.Fatalf("fetch complete article: %v", err)
			}
			if !utf8.ValidString(got.Content) || len(got.Content) <= 50_000 {
				t.Errorf("complete body has %d bytes, valid UTF-8 = %t", len(got.Content), utf8.ValidString(got.Content))
			}
			if countGot := strings.Count(got.Content, paragraph); countGot != count {
				t.Errorf("kept %d of %d paragraphs", countGot, count)
			}
			for _, want := range []string{
				"func finalExample() {\n    println(\"complete code\")\n}",
				"[Final reference](" + server.URL + "/references/final)",
				"![Final figure](" + server.URL + "/figures/final.png)",
				"COMPLETE ARTICLE SUFFIX 正文结束",
			} {
				if !strings.Contains(got.Content, want) {
					t.Errorf("complete article lost final marker %q", want)
				}
			}
		})
	}
}

func TestContentFetcherPreservesCompleteLongReaderArticles(t *testing.T) {
	for _, paragraph := range []string{
		"The complete English article preserves every paragraph and its final references.",
		"完整的中文正文必须保留每一个段落以及结尾的代码、图片和参考链接。",
	} {
		for _, route := range []string{"zai", "jina"} {
			t.Run(route+"/"+paragraph[:3], func(t *testing.T) {
				want := "# Complete Reader article\n\n" + strings.Repeat(paragraph+"\n\n", 900) +
					"```go\nfunc finalExample() {\n    println(\"complete code\")\n}\n```\n\n" +
					"[Final reference][final-source]\n\n![Final figure][final-figure]\n\n" +
					"[final-source]: https://example.org/references/final\n" +
					"[final-figure]: https://example.org/figures/final.png\n\nCOMPLETE ARTICLE SUFFIX 正文结束"
				directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
				defer directServer.Close()
				fetcher := &ContentFetcher{client: directServer.Client()}
				if route == "zai" {
					readerServer := newContentReaderServer(t, func(map[string]any) map[string]any {
						nested, _ := json.Marshal(map[string]string{"content": want})
						return toolResult(false, string(nested))
					})
					defer readerServer.Close()
					fetcher.reader = newZAIReader("reader-key", readerServer.URL, fetcher.client)
				} else {
					readerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						_, _ = io.WriteString(w, want)
					}))
					defer readerServer.Close()
					fetcher.jinaBaseURL = readerServer.URL + "/"
				}
				got, err := fetcher.FetchContent(context.Background(), directServer.URL+"/article")
				if err != nil {
					t.Fatalf("fetch complete Reader article: %v", err)
				}
				if got != want {
					t.Errorf("Reader article incomplete: got %d bytes, want all %d bytes; suffix present = %t", len(got), len(want), strings.HasSuffix(got, "COMPLETE ARTICLE SUFFIX 正文结束"))
				}
			})
		}
	}
}

func TestJinaRejectsIncompleteResponses(t *testing.T) {
	for _, scenario := range []string{"over transport limit", "interrupted body"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				body := strings.Repeat("complete article paragraph ", 8_000)
				if scenario == "interrupted body" {
					body = "# Valid prefix\n\nThis is not the complete article."
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			fetcher := &ContentFetcher{client: server.Client(), jinaBaseURL: server.URL + "/"}
			got, err := fetcher.fetchViaJina(context.Background(), "https://example.org/article")
			if err == nil || got != "" {
				t.Fatalf("incomplete response accepted: bytes = %d, error = %v", len(got), err)
			}
		})
	}
}

func TestContentFetcherRejectsInterruptedDirectArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := "<article><p>" + strings.Repeat("valid article prefix ", 50) + "</p></article>"
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	got, _, err := (&ContentFetcher{client: server.Client()}).fetchDirect(context.Background(), server.URL)
	if err == nil || got.Content != "" {
		t.Fatalf("interrupted direct response accepted: bytes = %d, error = %v", len(got.Content), err)
	}
}
