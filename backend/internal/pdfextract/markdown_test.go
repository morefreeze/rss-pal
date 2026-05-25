package pdfextract

import (
	"strings"
	"testing"
)

func TestAssembleMarkdown_PerPageSections(t *testing.T) {
	pages := []PageContent{
		{
			PageNum: 1,
			Text:    "Page one body.",
			Images: []ImageRef{
				{Idx: 0, Format: "png"},
				{Idx: 1, Format: "jpg"},
			},
		},
		{
			PageNum: 2,
			Text:    "Page two body.",
			Images:  nil,
		},
		{
			PageNum: 3,
			Text:    "", // empty page — should be skipped entirely
			Images:  nil,
		},
		{
			PageNum: 4,
			Text:    "",
			Images:  []ImageRef{{Idx: 2, Format: "png"}}, // image-only page kept
		},
	}
	md := assembleMarkdown(pages, 42, 0, 0)

	want := []string{
		"## 第 1 页",
		"Page one body.",
		"![](/api/articles/42/images/0.png)",
		"![](/api/articles/42/images/1.jpg)",
		"## 第 2 页",
		"Page two body.",
		"## 第 4 页",
		"![](/api/articles/42/images/2.png)",
	}
	for _, s := range want {
		if !strings.Contains(md, s) {
			t.Errorf("missing %q in markdown:\n%s", s, md)
		}
	}
	if strings.Contains(md, "## 第 3 页") {
		t.Error("empty page 3 should not appear")
	}
	if strings.Contains(md, "注：原 PDF") {
		t.Error("no truncation footer expected when total=kept")
	}
}

func TestAssembleMarkdown_TruncationFooter(t *testing.T) {
	pages := []PageContent{{PageNum: 1, Text: "body", Images: []ImageRef{{Idx: 0, Format: "png"}}}}
	md := assembleMarkdown(pages, 1, 150 /* original */, 100 /* kept */)
	if !strings.Contains(md, "注：原 PDF 共 150 张图（去重后 100 张），超出 100 张限制，已省略后 50 张。") {
		t.Errorf("missing truncation footer:\n%s", md)
	}
}
