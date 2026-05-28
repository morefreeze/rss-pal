package ai

import (
	"reflect"
	"testing"
)

func TestExtractImageURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no images", "just some text", nil},
		{"single", "before ![](https://a.com/x.png) after", []string{"https://a.com/x.png"}},
		{"with alt", "![cat](https://a.com/x.png)", []string{"https://a.com/x.png"}},
		{"preserves order, dedupes nothing", "![](https://a.com/1.png) ![](https://a.com/2.png) ![](https://a.com/1.png)",
			[]string{"https://a.com/1.png", "https://a.com/2.png", "https://a.com/1.png"}},
		{"local article-images URL", "![](/api/articles/42/images/0.png)", []string{"/api/articles/42/images/0.png"}},
		{"url with parens encoded", "![](https://a.com/x.png?q=1&x=y)", []string{"https://a.com/x.png?q=1&x=y"}},
		{"newlines around", "intro\n\n![](https://a.com/x.png)\n\nmore", []string{"https://a.com/x.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractImageURLs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %#v got %#v", tc.want, got)
			}
		})
	}
}

func TestCountTextRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"plain ascii", "hello world", 11},
		{"chinese", "你好世界", 4},
		{"strips image alts and urls", "你好![alt](https://a.com/x.png)再见", 2 + 2}, // "你好" + "再见"
		{"only images", "![](https://a.com/1.png)![](https://b.com/2.png)", 0},
		{"newlines kept as runes", "line1\nline2", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CountTextRunes(tc.in)
			if got != tc.want {
				t.Fatalf("want=%d got=%d (input %q)", tc.want, got, tc.in)
			}
		})
	}
}
