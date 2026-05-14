package util

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips fragment", "https://example.com/a#section", "https://example.com/a"},
		{"strips utm_source", "https://example.com/a?utm_source=x", "https://example.com/a"},
		{"strips multiple utm_*", "https://example.com/a?utm_source=x&utm_medium=y", "https://example.com/a"},
		{"strips gclid", "https://example.com/a?gclid=xx", "https://example.com/a"},
		{"strips fbclid", "https://example.com/a?fbclid=xx", "https://example.com/a"},
		{"keeps non-tracking params", "https://example.com/a?id=1&utm_medium=y", "https://example.com/a?id=1"},
		{"keeps multiple non-tracking params in order", "https://example.com/a?id=1&utm_medium=y&id2=2", "https://example.com/a?id=1&id2=2"},
		{"strips fragment and tracking", "https://Example.com/a?utm_source=x&id=1#sec", "https://example.com/a?id=1"},
		{"lowercases host", "https://EXAMPLE.com/path", "https://example.com/path"},
		{"preserves path case", "https://example.com/Article/Foo", "https://example.com/Article/Foo"},
		{"preserves trailing slash", "https://example.com/a/", "https://example.com/a/"},
		{"preserves http scheme", "http://example.com/a", "http://example.com/a"},
		{"clean url unchanged", "https://example.com/a", "https://example.com/a"},
		{"unparseable returned as-is", "not a url", "not a url"},
		{"empty string returned as-is", "", ""},
		{"strips ref and ref_src", "https://example.com/a?ref=foo&ref_src=bar&id=1", "https://example.com/a?id=1"},
		{"strips msclkid yclid igshid", "https://example.com/a?msclkid=1&yclid=2&igshid=3", "https://example.com/a"},
		{"strips _hsenc _hsmi mc_cid mc_eid", "https://example.com/a?_hsenc=1&_hsmi=2&mc_cid=3&mc_eid=4", "https://example.com/a"},
		{"rewrites twitter.com host to x.com", "https://twitter.com/x/status/1", "https://x.com/x/status/1"},
		{"rewrites mobile.twitter.com host to x.com", "https://mobile.twitter.com/x/status/1", "https://x.com/x/status/1"},
		{"rewrites www.twitter.com host to x.com", "https://www.twitter.com/x/status/1", "https://x.com/x/status/1"},
		{"rewrites www.x.com host to x.com", "https://www.x.com/x/status/1", "https://x.com/x/status/1"},
		{"strips share-tracking query on x.com status path", "https://x.com/x/status/1?s=20", "https://x.com/x/status/1"},
		{"strips multi-key share-tracking query", "https://x.com/x/status/1?t=abc&s=20", "https://x.com/x/status/1"},
		{"keeps query on non-status x.com path", "https://x.com/search?q=go", "https://x.com/search?q=go"},
		{"preserves handle casing on status path", "https://x.com/Karpathy/status/2053872850101285137", "https://x.com/Karpathy/status/2053872850101285137"},
		{"twitter.com profile path keeps existing rules", "https://twitter.com/karpathy?ref_src=x", "https://x.com/karpathy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURL(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
