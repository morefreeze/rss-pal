package rss

import "testing"

func TestIsAvatarImageURL(t *testing.T) {
	cases := []struct {
		name string
		src  string
		alt  string
		want bool
	}{
		{"gravatar host", "https://www.gravatar.com/avatar/abc123", "", true},
		{"avatar in path", "https://cdn.example.com/avatars/u123.png", "", true},
		{"avatar substring path", "https://example.com/some/avatar/x.png", "", true},
		{"alt avatar keyword", "https://example.com/x.png", "author avatar", true},
		{"alt headshot", "https://example.com/x.png", "headshot of jane", true},
		{"normal image, plain alt", "https://example.com/cat.png", "a cat", false},
		{"normal image, empty alt", "https://example.com/cat.png", "", false},
		{"case insensitive — uppercase Avatar in alt", "https://example.com/x.png", "AVATAR", true},
		{"case insensitive — capitalized GRAVATAR in URL", "https://www.GRAVATAR.com/avatar/abc", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAvatarImageURL(tc.src, tc.alt)
			if got != tc.want {
				t.Fatalf("src=%q alt=%q want=%v got=%v", tc.src, tc.alt, tc.want, got)
			}
		})
	}
}
