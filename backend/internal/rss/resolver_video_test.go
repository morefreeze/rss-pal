package rss

import "testing"

func TestResolveFeedURLVideoPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "bilibili_user", input: "https://space.bilibili.com/14064034", rsshubBase: base, want: base + "/bilibili/user/video/14064034"},
		{name: "bilibili_user_video", input: "https://www.space.bilibili.com/14064034/video/?from=search#top", rsshubBase: base, want: base + "/bilibili/user/video/14064034"},
		{name: "bilibili_non_numeric", input: "https://space.bilibili.com/not-a-uid", rsshubBase: base, want: "https://space.bilibili.com/not-a-uid"},
		{name: "bilibili_dynamic", input: "https://space.bilibili.com/14064034/dynamic", rsshubBase: base, want: "https://space.bilibili.com/14064034/dynamic"},

		{name: "youtube_channel_root_native", input: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q", rsshubBase: base, want: "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q"},
		{name: "youtube_channel_videos_native", input: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/videos", rsshubBase: base, want: "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q"},
		{name: "youtube_channel_live_passthrough", input: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/live", rsshubBase: base, want: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/live"},
		{name: "youtube_channel_about_passthrough", input: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/about", rsshubBase: base, want: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/about"},
		{name: "youtube_playlist_native", input: "https://youtube.com/playlist?list=PL123&utm_source=test", rsshubBase: base, want: "https://www.youtube.com/feeds/videos.xml?playlist_id=PL123"},
		{name: "youtube_handle", input: "https://youtube.com/@Fireship/videos?view=0", rsshubBase: base, want: base + "/youtube/user/@Fireship"},
		{name: "youtube_user", input: "https://m.youtube.com/user/GoogleDevelopers", rsshubBase: base, want: base + "/youtube/user/GoogleDevelopers"},
		{name: "youtube_custom", input: "https://www.youtube.com/c/Computerphile", rsshubBase: base, want: base + "/youtube/c/Computerphile"},
		{name: "youtube_watch_passthrough", input: "https://www.youtube.com/watch?v=abc", rsshubBase: base, want: "https://www.youtube.com/watch?v=abc"},
		{name: "youtube_shorts_passthrough", input: "https://www.youtube.com/shorts/abc", rsshubBase: base, want: "https://www.youtube.com/shorts/abc"},
		{name: "youtube_playlist_missing_id", input: "https://www.youtube.com/playlist", rsshubBase: base, want: "https://www.youtube.com/playlist"},

		{name: "douyin_user", input: "https://www.douyin.com/user/MS4wLjABAAAAexample?from_tab_name=main", rsshubBase: base, want: base + "/douyin/user/MS4wLjABAAAAexample"},
		{name: "douyin_hashtag", input: "https://www.douyin.com/hashtag/123456", rsshubBase: base, want: base + "/douyin/hashtag/123456"},
		{name: "douyin_vertical_tab_passthrough", input: "https://www.douyin.com/hashtag/%0B", rsshubBase: base, want: "https://www.douyin.com/hashtag/%0B"},
		{name: "douyin_control_passthrough", input: "https://www.douyin.com/hashtag/%7F", rsshubBase: base, want: "https://www.douyin.com/hashtag/%7F"},
		{name: "douyin_unicode_whitespace_passthrough", input: "https://www.douyin.com/hashtag/%C2%A0", rsshubBase: base, want: "https://www.douyin.com/hashtag/%C2%A0"},
		{name: "douyin_dot_segment_passthrough", input: "https://www.douyin.com/hashtag/%2E%2E", rsshubBase: base, want: "https://www.douyin.com/hashtag/%2E%2E"},
		{name: "douyin_reserved_character_escaped", input: "https://www.douyin.com/hashtag/topic%3Fname", rsshubBase: base, want: base + "/douyin/hashtag/topic%3Fname"},
		{name: "douyin_live", input: "https://live.douyin.com/987654", rsshubBase: base, want: base + "/douyin/live/987654"},
		{name: "douyin_video_passthrough", input: "https://www.douyin.com/video/123", rsshubBase: base, want: "https://www.douyin.com/video/123"},
		{name: "douyin_invalid_user", input: "https://www.douyin.com/user/short-id", rsshubBase: base, want: "https://www.douyin.com/user/short-id"},

		{name: "tiktok_user", input: "https://www.tiktok.com/@linustech/", rsshubBase: base, want: base + "/tiktok/user/@linustech"},
		{name: "tiktok_video_passthrough", input: "https://www.tiktok.com/@linustech/video/123", rsshubBase: base, want: "https://www.tiktok.com/@linustech/video/123"},
		{name: "tiktok_live_passthrough", input: "https://www.tiktok.com/@linustech/live", rsshubBase: base, want: "https://www.tiktok.com/@linustech/live"},
	})
}
