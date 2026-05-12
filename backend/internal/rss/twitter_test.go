package rss

import "testing"

func TestIsTwitterStatusURL(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantID string
		wantOK bool
	}{
		{"x.com status", "https://x.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"twitter.com status", "https://twitter.com/x/status/1", "1", true},
		{"mobile.twitter.com status", "https://mobile.twitter.com/x/status/42", "42", true},
		{"www.x.com status", "https://www.x.com/x/status/9", "9", true},
		{"uppercase host", "https://X.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"trailing slash", "https://x.com/karpathy/status/123/", "123", true},
		{"with query (already normalized away, but accept)", "https://x.com/x/status/1?s=20", "1", true},
		{"profile page", "https://x.com/karpathy", "", false},
		{"with_replies", "https://x.com/karpathy/with_replies", "", false},
		{"search page", "https://x.com/search?q=go", "", false},
		{"lists page", "https://x.com/i/lists/1234", "", false},
		{"non-numeric status id", "https://x.com/karpathy/status/abc", "", false},
		{"status without id", "https://x.com/karpathy/status/", "", false},
		{"non-twitter host", "https://example.com/karpathy/status/1", "", false},
		{"empty", "", "", false},
		{"unparseable", "not a url", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := IsTwitterStatusURL(tt.in)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("IsTwitterStatusURL(%q) = (%q, %v); want (%q, %v)", tt.in, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}
