package rss

import "testing"

func TestResolveFeedURLContentPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "csdn_blog", input: "https://blog.csdn.net/csdngeeknews", rsshubBase: base, want: base + "/csdn/blog/csdngeeknews"},
		{name: "csdn_article_to_author", input: "https://blog.csdn.net/csdngeeknews/article/details/123?spm=1001", rsshubBase: base, want: base + "/csdn/blog/csdngeeknews"},
		{name: "csdn_reserved_nav", input: "https://blog.csdn.net/nav", rsshubBase: base, want: "https://blog.csdn.net/nav"},
		{name: "csdn_reserved_case_insensitive", input: "https://blog.csdn.net/NAV", rsshubBase: base, want: "https://blog.csdn.net/NAV"},
		{name: "csdn_reserved_near_match", input: "https://blog.csdn.net/navigator", rsshubBase: base, want: base + "/csdn/blog/navigator"},
		{name: "csdn_root", input: "https://blog.csdn.net/", rsshubBase: base, want: "https://blog.csdn.net/"},

		{name: "github_user", input: "https://github.com/DIYgod", rsshubBase: base, want: base + "/github/activity/DIYgod"},
		{name: "github_repo", input: "https://github.com/DIYgod/RSSHub", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_repo_git_suffix", input: "https://github.com/DIYgod/RSSHub.git", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_repo_one_git_suffix", input: "https://github.com/DIYgod/RSSHub.git.git", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub.git"},
		{name: "github_repo_empty_after_git_suffix", input: "https://github.com/DIYgod/.git", rsshubBase: base, want: "https://github.com/DIYgod/.git"},
		{name: "github_repo_subpage", input: "https://github.com/DIYgod/RSSHub/issues/123", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_reserved_settings", input: "https://github.com/settings", rsshubBase: base, want: "https://github.com/settings"},
		{name: "github_dashboard_passthrough", input: "https://github.com/dashboard", rsshubBase: base, want: "https://github.com/dashboard"},
		{name: "github_signup_passthrough", input: "https://github.com/signup", rsshubBase: base, want: "https://github.com/signup"},
		{name: "github_codespaces_passthrough", input: "https://github.com/codespaces", rsshubBase: base, want: "https://github.com/codespaces"},
		{name: "github_copilot_passthrough", input: "https://github.com/copilot", rsshubBase: base, want: "https://github.com/copilot"},
		{name: "github_reserved_case_insensitive", input: "https://github.com/Settings", rsshubBase: base, want: "https://github.com/Settings"},
		{name: "github_reserved_near_match", input: "https://github.com/settings-team", rsshubBase: base, want: base + "/github/activity/settings-team"},
		{name: "github_reserved_orgs", input: "https://github.com/orgs/openai", rsshubBase: base, want: "https://github.com/orgs/openai"},
		{name: "github_unsafe_owner", input: "https://github.com/%7F", rsshubBase: base, want: "https://github.com/%7F"},
		{name: "github_root", input: "https://github.com/", rsshubBase: base, want: "https://github.com/"},
	})
}
