package rss

import "testing"

func TestNetworkFeedURLRejectsLegacySentinels(t *testing.T) {
	for _, s := range []string{"bookmarklet://user/1", "extension://twitter:user/1/a", "https://extension://twitter:user/1/a", "https://bookmarklet://user/1", "https://", "ftp://example.com/rss"} {
		if IsNetworkFeedURL(s) {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{"https://example.com/feed", "http://127.0.0.1:1234/feed", "https://example.org/rss?target=https://other.org"} {
		if !IsNetworkFeedURL(s) {
			t.Errorf("rejected %q", s)
		}
	}
}
