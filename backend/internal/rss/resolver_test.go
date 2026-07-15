package rss

import "testing"

type resolveCase struct {
	name       string
	input      string
	rsshubBase string
	want       string
}

func runResolveCases(t *testing.T, cases []resolveCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFeedURL(tc.input, tc.rsshubBase); got != tc.want {
				t.Fatalf("ResolveFeedURL(%q, %q) = %q, want %q", tc.input, tc.rsshubBase, got, tc.want)
			}
		})
	}
}

func TestResolveFeedURLCore(t *testing.T) {
	runResolveCases(t, []resolveCase{
		{name: "empty_base", input: "https://www.youtube.com/channel/UC123", rsshubBase: "", want: "https://www.youtube.com/channel/UC123"},
		{name: "empty_input", input: "", rsshubBase: "http://rsshub:1200", want: ""},
		{name: "malformed_input", input: "://nope", rsshubBase: "http://rsshub:1200", want: "://nope"},
		{name: "relative_input", input: "/user/123", rsshubBase: "http://rsshub:1200", want: "/user/123"},
		{name: "non_http_scheme", input: "ftp://space.bilibili.com/14064034", rsshubBase: "http://rsshub:1200", want: "ftp://space.bilibili.com/14064034"},
		{name: "existing_rsshub_url", input: "http://rsshub:1200/weibo/user/1195230310", rsshubBase: "http://rsshub:1200/", want: "http://rsshub:1200/weibo/user/1195230310"},
		{name: "unmatched_host", input: "https://example.com/blog", rsshubBase: "http://rsshub:1200", want: "https://example.com/blog"},
	})
}
