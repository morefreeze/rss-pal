package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsWeChatURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://mp.weixin.qq.com/s/abc", true},
		{"https://mp.weixin.qq.com/s?__biz=MzI=&mid=1", true},
		{"http://mp.weixin.qq.com/s/abc", true},
		{"https://MP.WEIXIN.QQ.COM/s/abc", true},
		{"https://weixin.qq.com/", false},
		{"https://example.com/mp.weixin.qq.com/fake", false},
		{"", false},
		{"://nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := IsWeChatURL(tc.in); got != tc.want {
				t.Errorf("IsWeChatURL(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractBizFromQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "https://mp.weixin.qq.com/s?__biz=MzI5MDA1MDU4MA==&mid=1", "MzI5MDA1MDU4MA=="},
		{"url_encoded", "https://mp.weixin.qq.com/s?__biz=MzI5MDA1MDU4MA%3D%3D&mid=1", "MzI5MDA1MDU4MA=="},
		{"short_link_no_biz", "https://mp.weixin.qq.com/s/abc", ""},
		{"empty_biz", "https://mp.weixin.qq.com/s?__biz=", ""},
		{"malformed", "://nope", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractBizFromQuery(tc.in); got != tc.want {
				t.Errorf("extractBizFromQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractBiz_QueryFastPath(t *testing.T) {
	// Should not need an HTTP client when __biz is already in the URL.
	got, err := ExtractBiz(context.Background(), nil,
		"https://mp.weixin.qq.com/s?__biz=MzI5MDA1MDU4MA==&mid=1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "MzI5MDA1MDU4MA==" {
		t.Errorf("got biz=%q, want MzI5MDA1MDU4MA==", got)
	}
}

func TestExtractBiz_HTMLFallback(t *testing.T) {
	// Mock a mp.weixin.qq.com short link that returns HTML containing the
	// `var biz = "..."` snippet WeChat actually ships.
	html := `<html><head><script>
		var first_sceen__time = 1;
		var biz = "MzI5MDA1MDU4MA==" || "";
		var sn = "abc";
	</script></head><body>article</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	got, err := ExtractBiz(context.Background(), srv.Client(), srv.URL+"/s/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "MzI5MDA1MDU4MA==" {
		t.Errorf("got biz=%q, want MzI5MDA1MDU4MA==", got)
	}
}

func TestExtractBiz_SingleQuotedHTML(t *testing.T) {
	html := `<script>var biz='MzAbCdEf==';</script>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	got, err := ExtractBiz(context.Background(), srv.Client(), srv.URL+"/s/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "MzAbCdEf==" {
		t.Errorf("got biz=%q, want MzAbCdEf==", got)
	}
}

func TestExtractBiz_HTMLMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>no biz here</body></html>`))
	}))
	defer srv.Close()

	_, err := ExtractBiz(context.Background(), srv.Client(), srv.URL+"/s/abc")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestExtractBiz_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := ExtractBiz(context.Background(), srv.Client(), srv.URL+"/s/abc")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("expected status-403 error, got %v", err)
	}
}

func TestExtractBiz_NoClientShortLink(t *testing.T) {
	// Short link without query-string biz and a nil client should fail fast
	// — this is the direct-add path (POST /api/feeds) where we skip HTTP
	// to keep the request snappy.
	_, err := ExtractBiz(context.Background(), nil, "https://mp.weixin.qq.com/s/abc")
	if err == nil || !strings.Contains(err.Error(), "no http client") {
		t.Errorf("expected no-client error, got %v", err)
	}
}

func TestBuildFeedURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		biz  string
		want string
	}{
		{"plain", "http://rsshub:1200", "MzI5MDA1MDU4MA==", "http://rsshub:1200/wechat/ce/MzI5MDA1MDU4MA=="},
		{"trailing_slash", "http://rsshub:1200/", "MzI5MDA1MDU4MA==", "http://rsshub:1200/wechat/ce/MzI5MDA1MDU4MA=="},
		{"empty_base", "", "biz", ""},
		{"empty_biz", "http://rsshub:1200", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildFeedURL(tc.base, tc.biz); got != tc.want {
				t.Errorf("BuildFeedURL(%q, %q) = %q, want %q", tc.base, tc.biz, got, tc.want)
			}
		})
	}
}
