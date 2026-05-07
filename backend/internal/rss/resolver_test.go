package rss

import "testing"

func TestResolveFeedURL(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		rsshubBase string
		want       string
	}{
		{"bilibili_user_root", "https://space.bilibili.com/14064034", "http://rsshub:1200", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"bilibili_user_video", "https://space.bilibili.com/14064034/video", "http://rsshub:1200", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"bilibili_user_video_slash", "https://space.bilibili.com/14064034/video/", "http://rsshub:1200", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"bilibili_user_trailing_slash", "https://space.bilibili.com/14064034/", "http://rsshub:1200", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"trims_trailing_slash_on_base", "https://space.bilibili.com/14064034", "http://rsshub:1200/", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"www_prefix", "https://www.space.bilibili.com/14064034", "http://rsshub:1200", "http://rsshub:1200/bilibili/user/video/14064034"},
		{"non_numeric_uid", "https://space.bilibili.com/abc", "http://rsshub:1200", "https://space.bilibili.com/abc"},
		{"deep_path_unsupported", "https://space.bilibili.com/14064034/dynamic", "http://rsshub:1200", "https://space.bilibili.com/14064034/dynamic"},
		{"different_host_passthrough", "https://www.youtube.com/watch?v=abc", "http://rsshub:1200", "https://www.youtube.com/watch?v=abc"},
		{"empty_base_passthrough", "https://space.bilibili.com/14064034", "", "https://space.bilibili.com/14064034"},
		{"empty_input", "", "http://rsshub:1200", ""},
		{"malformed_input", "://nope", "http://rsshub:1200", "://nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFeedURL(tc.in, tc.rsshubBase); got != tc.want {
				t.Errorf("ResolveFeedURL(%q, %q) = %q, want %q", tc.in, tc.rsshubBase, got, tc.want)
			}
		})
	}
}
