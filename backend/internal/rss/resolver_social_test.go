package rss

import "testing"

func TestResolveFeedURLSocialPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "weibo_desktop", input: "https://weibo.com/u/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
		{name: "weibo_mobile_u", input: "https://m.weibo.cn/u/1195230310?jumpfrom=weibocom", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
		{name: "weibo_mobile_profile", input: "https://m.weibo.cn/profile/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
		{name: "weibo_non_numeric", input: "https://weibo.com/u/not-a-uid", rsshubBase: base, want: "https://weibo.com/u/not-a-uid"},
		{name: "weibo_status_passthrough", input: "https://weibo.com/1195230310/P123", rsshubBase: base, want: "https://weibo.com/1195230310/P123"},

		{name: "zhihu_people", input: "https://www.zhihu.com/people/diygod", rsshubBase: base, want: base + "/zhihu/people/activities/diygod"},
		{name: "zhihu_people_activities", input: "https://www.zhihu.com/people/diygod/activities", rsshubBase: base, want: base + "/zhihu/people/activities/diygod"},
		{name: "zhihu_people_answers", input: "https://www.zhihu.com/people/diygod/answers", rsshubBase: base, want: base + "/zhihu/people/answers/diygod"},
		{name: "zhihu_question", input: "https://www.zhihu.com/question/123456", rsshubBase: base, want: base + "/zhihu/question/123456"},
		{name: "zhihu_topic", input: "https://www.zhihu.com/topic/19550517", rsshubBase: base, want: base + "/zhihu/topic/19550517"},
		{name: "zhihu_question_non_numeric", input: "https://www.zhihu.com/question/waiting", rsshubBase: base, want: "https://www.zhihu.com/question/waiting"},
		{name: "zhihu_topic_non_numeric", input: "https://www.zhihu.com/topic/not-a-topic-id", rsshubBase: base, want: "https://www.zhihu.com/topic/not-a-topic-id"},
		{name: "zhihu_answer_passthrough", input: "https://www.zhihu.com/question/123456/answer/789", rsshubBase: base, want: "https://www.zhihu.com/question/123456/answer/789"},
		{name: "zhihu_article_passthrough", input: "https://zhuanlan.zhihu.com/p/123", rsshubBase: base, want: "https://zhuanlan.zhihu.com/p/123"},

		{name: "wechat_homepage", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D&hid=16", rsshubBase: base, want: base + "/wechat/mp/homepage/MzA3MDM3NjE5NQ==/16"},
		{name: "wechat_homepage_category", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D&hid=16&cid=2", rsshubBase: base, want: base + "/wechat/mp/homepage/MzA3MDM3NjE5NQ==/16/2"},
		{name: "wechat_article_passthrough", input: "https://mp.weixin.qq.com/s/kHGSiyxTf8J4ZxmJLM2QJQ", rsshubBase: base, want: "https://mp.weixin.qq.com/s/kHGSiyxTf8J4ZxmJLM2QJQ"},
		{name: "wechat_missing_hid", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D", rsshubBase: base, want: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D"},

		{name: "xiaohongshu_user", input: "https://www.xiaohongshu.com/user/profile/593032945e87e77791e03696?xsec_token=secret", rsshubBase: base, want: base + "/xiaohongshu/user/593032945e87e77791e03696/notes"},
		{name: "xiaohongshu_note_passthrough", input: "https://www.xiaohongshu.com/explore/123", rsshubBase: base, want: "https://www.xiaohongshu.com/explore/123"},
		{name: "xiaohongshu_missing_user", input: "https://www.xiaohongshu.com/user/profile/", rsshubBase: base, want: "https://www.xiaohongshu.com/user/profile/"},
	})
}
