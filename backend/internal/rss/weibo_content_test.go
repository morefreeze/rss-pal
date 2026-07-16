package rss

import (
	"strings"
	"testing"
)

const weiboArticleURL = "https://weibo.com/2904546111/R8PkkgPKd"

func TestBuildItemContentExtractsFirstBloggerCommentAndDirectLinks(t *testing.T) {
	description := `<div class="post">
		<p>3784 正文内容</p>
		<p><img src="https://wx1.sinaimg.cn/large/post.jpg" alt="海报"></p>
	</div>
	<br clear="both"><div style="clear: both"></div>
	<div class="hot-comments">
		<h3> 热门评论 </h3>
		<p><a href="https://weibo.com/2904546111" target="_blank">原博主</a>:
			资源见 <a href="https://weibo.cn/sinaurl?u=https%3A%2F%2Fpan.quark.cn%2Fs%2Fc140bc08bbfa">夸克</a>
			和 <a href="https://weibo.cn/sinaurl?u=https%3A%2F%2Fpan.baidu.com%2Fs%2F1tg0ec1MDYlS8B0Ph7fVDsA%3Fpwd%3Dir22">百度网盘</a>
			<blockquote><div><a href="https://weibo.com/2904546111">原博主</a>: 拿走吱吱吱</div></blockquote>
		</p>
		<p><a href="https://weibo.com/99887766">桜吹雪Freedom</a>: 谢谢分享</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if !enriched {
		t.Fatal("BuildItemContent() enriched = false, want true")
	}
	for _, want := range []string{
		"3784 正文内容",
		"![海报](https://wx1.sinaimg.cn/large/post.jpg)",
		"### 博主首评",
		"[夸克](https://pan.quark.cn/s/c140bc08bbfa)",
		"[百度网盘](https://pan.baidu.com/s/1tg0ec1MDYlS8B0Ph7fVDsA?pwd=ir22)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("BuildItemContent() missing %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"热门评论", "桜吹雪Freedom", "拿走吱吱吱", "weibo.cn/sinaurl"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("BuildItemContent() contains excluded %q:\n%s", unwanted, content)
		}
	}
}

func TestBuildItemContentCanonicalizesWeiboHosts(t *testing.T) {
	description := `<div><p>微博正文</p></div>
	<div class="hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://www.weibo.com:443/2904546111">原博主</a>:
			<a href="https://www.weibo.cn:443/sinaurl?u=https%3A%2F%2Fdownloads.example.com%2Fresource">直接资料</a>
		</p>
	</div>`
	itemURL := "https://www.weibo.com:443/2904546111/R8PkkgPKd"

	content, enriched := BuildItemContent(description, "unused fallback", itemURL)

	if !enriched {
		t.Fatal("BuildItemContent() enriched = false, want true")
	}
	for _, want := range []string{
		"### 博主首评",
		"[原博主](https://www.weibo.com:443/2904546111)",
		"[直接资料](https://downloads.example.com/resource)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("BuildItemContent() missing canonical-host content %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "weibo.cn:443/sinaurl") {
		t.Errorf("BuildItemContent() retained canonical redirect wrapper:\n%s", content)
	}
}

func TestBuildItemContentDropsCommentsWithoutSameAuthor(t *testing.T) {
	description := `<div><p>需要保留的微博正文</p></div>
	<div class="hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://weibo.com/99887766">其他用户</a>: 路过</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if enriched {
		t.Fatal("BuildItemContent() enriched = true, want false")
	}
	if !strings.Contains(content, "需要保留的微博正文") {
		t.Errorf("BuildItemContent() dropped post body:\n%s", content)
	}
	for _, unwanted := range []string{"热门评论", "其他用户", "路过"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("BuildItemContent() contains excluded %q:\n%s", unwanted, content)
		}
	}
}

func TestBuildItemContentRejectsNestedAuthorAnchor(t *testing.T) {
	description := `<div><p>需要保留的微博正文</p></div>
	<div class="hot-comments">
		<h3>热门评论</h3>
		<p><span><a href="https://weibo.com/2904546111">嵌套博主链接</a></span>: 不能当作顶层博主评论</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if enriched {
		t.Fatal("BuildItemContent() enriched = true, want false")
	}
	if !strings.Contains(content, "需要保留的微博正文") {
		t.Errorf("BuildItemContent() dropped post body:\n%s", content)
	}
	for _, unwanted := range []string{"博主首评", "嵌套博主链接", "不能当作顶层博主评论"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("BuildItemContent() contains nested-author comment %q:\n%s", unwanted, content)
		}
	}
}

func TestBuildItemContentKeepsOnlyFirstSameAuthorTopLevelComment(t *testing.T) {
	description := `<div><p>微博正文</p></div>
	<div class="hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://weibo.com/2904546111">原博主</a>: 第一条博主评论</p>
		<p><a href="https://weibo.com/2904546111">原博主</a>: 第二条博主评论</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if !enriched {
		t.Fatal("BuildItemContent() enriched = false, want true")
	}
	if !strings.Contains(content, "第一条博主评论") {
		t.Errorf("BuildItemContent() missing first comment:\n%s", content)
	}
	if strings.Contains(content, "第二条博主评论") {
		t.Errorf("BuildItemContent() retained second comment:\n%s", content)
	}
	if strings.Count(content, "### 博主首评") != 1 {
		t.Errorf("BuildItemContent() blogger heading count = %d, want 1:\n%s", strings.Count(content, "### 博主首评"), content)
	}
}

func TestBuildItemContentRemovesAllHotCommentWrappers(t *testing.T) {
	description := `<div><p>微博正文</p></div>
	<div class="first-hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://weibo.com/2904546111">原博主</a>: 第一处博主首评</p>
	</div>
	<div class="second-hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://weibo.com/99887766">其他用户</a>: 第二处其他评论</p>
		<p><a href="https://weibo.com/2904546111">原博主</a>: 第二处稍晚博主评论</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if !enriched {
		t.Fatal("BuildItemContent() enriched = false, want true")
	}
	if !strings.Contains(content, "第一处博主首评") {
		t.Errorf("BuildItemContent() missing first qualifying comment:\n%s", content)
	}
	if got := strings.Count(content, "### 博主首评"); got != 1 {
		t.Errorf("BuildItemContent() blogger heading count = %d, want 1:\n%s", got, content)
	}
	for _, unwanted := range []string{"热门评论", "第二处其他评论", "第二处稍晚博主评论"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("BuildItemContent() contains later wrapper content %q:\n%s", unwanted, content)
		}
	}
}

func TestBuildItemContentPreservesUnsafeOrMalformedRedirects(t *testing.T) {
	description := `<div><p>微博正文</p></div>
	<div class="hot-comments">
		<h3>热门评论</h3>
		<p><a href="https://weibo.com/2904546111">原博主</a>:
			<a href="https://weibo.cn/sinaurl?u=%zz">malformed</a>
			<a href="https://weibo.cn/sinaurl?u=%2Frelative">relative</a>
			<a href="https://weibo.cn/sinaurl?u=javascript%3Aalert%281%29">javascript</a>
			<a href="https://weibo.cn/sinaurl?u=ftp%3A%2F%2Ffiles.example.com%2Farchive">ftp</a>
		</p>
	</div>`

	content, enriched := BuildItemContent(description, "unused fallback", weiboArticleURL)

	if !enriched {
		t.Fatal("BuildItemContent() enriched = false, want true")
	}
	if got := strings.Count(content, "weibo.cn/sinaurl"); got != 4 {
		t.Errorf("BuildItemContent() preserved redirect count = %d, want 4:\n%s", got, content)
	}
	for _, want := range []string{
		"[malformed](https://weibo.cn/sinaurl?u=%zz)",
		"[relative](https://weibo.cn/sinaurl?u=%2Frelative)",
		"[javascript](https://weibo.cn/sinaurl?u=javascript%3Aalert%281%29)",
		"[ftp](https://weibo.cn/sinaurl?u=ftp%3A%2F%2Ffiles.example.com%2Farchive)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("BuildItemContent() did not preserve unsafe redirect %q:\n%s", want, content)
		}
	}
}

func TestBuildItemContentNonWeiboMatchesStripHTML(t *testing.T) {
	description := `<p>Hello <a href="https://example.com/ignored">world</a>.</p><p>Second  paragraph.</p>`
	want := StripHTML(description)

	content, enriched := BuildItemContent(description, "unused fallback", "https://example.com/articles/42")

	if enriched {
		t.Fatal("BuildItemContent() enriched = true, want false")
	}
	if content != want {
		t.Errorf("BuildItemContent() = %q, want exact StripHTML result %q", content, want)
	}
}

func TestBuildItemContentUsesFallbackWhenDescriptionBlank(t *testing.T) {
	fallback := `<div><p>fallback 字段正文</p><p><img src="https://example.com/fallback.png" alt="fallback"></p></div>`

	content, enriched := BuildItemContent(" \n\t ", fallback, weiboArticleURL)

	if enriched {
		t.Fatal("BuildItemContent() enriched = true, want false")
	}
	for _, want := range []string{"fallback 字段正文", "![fallback](https://example.com/fallback.png)"} {
		if !strings.Contains(content, want) {
			t.Errorf("BuildItemContent() did not use fallback, missing %q:\n%s", want, content)
		}
	}
}

func TestShouldDeepFetchArticle(t *testing.T) {
	tests := []struct {
		name      string
		feedType  string
		itemURL   string
		mediaType string
		want      bool
	}{
		{name: "desktop Weibo status", itemURL: "https://weibo.com/2904546111/R8PkkgPKd", want: false},
		{name: "www desktop Weibo status with default port", itemURL: "https://www.weibo.com:443/2904546111/R8PkkgPKd", want: false},
		{name: "mobile Weibo status", itemURL: "https://m.weibo.cn/status/5172375334583796", want: false},
		{name: "mobile Weibo status with default port", itemURL: "https://m.weibo.cn:443/status/5172375334583796", want: false},
		{name: "youtube feed", feedType: "youtube", itemURL: "https://example.com/watch", want: false},
		{name: "podcast feed", feedType: "podcast", itemURL: "https://example.com/episode", want: false},
		{name: "video media", itemURL: "https://example.com/video", mediaType: "video/mp4", want: false},
		{name: "normal article", feedType: "rss", itemURL: "https://example.com/articles/42", mediaType: "text/html", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldDeepFetchArticle(tt.feedType, tt.itemURL, tt.mediaType); got != tt.want {
				t.Errorf("ShouldDeepFetchArticle(%q, %q, %q) = %t, want %t", tt.feedType, tt.itemURL, tt.mediaType, got, tt.want)
			}
		})
	}
}
