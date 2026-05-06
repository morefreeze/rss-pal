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
