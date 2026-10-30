package api

import (
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/model"
	"strings"
	"testing"
)

func TestSearchSnippetCentersBodyMatchAndKeepsLeanPayload(t *testing.T) {
	a := model.Article{Content: strings.Repeat("前文", 200) + "匹配关键词" + strings.Repeat("后文", 200), SummaryBrief: "不相关摘要"}
	item := articleToSearchItem(a, "关键词")
	if !strings.Contains(item.SearchSnippet, "匹配关键词") || !strings.HasPrefix(item.SearchSnippet, "…") || len([]rune(item.SearchSnippet)) > 182 {
		t.Fatalf("bad snippet: %s", item.SearchSnippet)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"content":`, `"summary_detailed":`} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("fat payload: %s", data)
		}
	}
}
func TestSearchSnippetUsesVisibleMarkdownAndSourceFallback(t *testing.T) {
	a := model.Article{Content: "before **MATCH** [label](https://hidden.example) after", SummaryBrief: "brief"}
	if got := articleToSearchItem(a, "match").SearchSnippet; got != "before MATCH label after" {
		t.Fatalf("got %q", got)
	}
	a = model.Article{URL: "https://example.com/needle", SummaryBrief: "brief"}
	if got := articleToSearchItem(a, "needle").SearchSnippet; !strings.Contains(got, "needle") {
		t.Fatalf("got %q", got)
	}
}

func TestSearchSnippetCentersPinyinBriefMatch(t *testing.T) {
	a := model.Article{SummaryBrief: strings.Repeat("很长的摘要文字。", 40) + "科技周刊内容"}
	got := articleToSearchItem(a, "keji").SearchSnippet
	if !strings.Contains(got, "科技") {
		t.Fatalf("missing pinyin context: %q", got)
	}
}
