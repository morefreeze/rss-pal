package repository

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestPinyinContainsMatchesChineseText(t *testing.T) {
	if !pinyinContains("科技爱好者周刊", "keji") {
		t.Fatalf("pinyinContains did not match keji against 科技爱好者周刊")
	}
}

func TestPinyinContainsMatchesInitials(t *testing.T) {
	if !pinyinContains("阮一峰的网络日志", "ryf") {
		t.Fatalf("pinyinContains did not match ryf against 阮一峰的网络日志")
	}
}

func TestArticleMatchesPinyinSearchUsesTitleAndFeedTitle(t *testing.T) {
	article := model.Article{
		Title:     "科技爱好者周刊（第 301 期）",
		FeedTitle: "阮一峰的网络日志",
	}
	if !articleMatchesPinyinSearch(article, "keji") {
		t.Fatalf("articleMatchesPinyinSearch did not match article title pinyin")
	}
	if !articleMatchesPinyinSearch(article, "ryf") {
		t.Fatalf("articleMatchesPinyinSearch did not match feed title initials")
	}
}
