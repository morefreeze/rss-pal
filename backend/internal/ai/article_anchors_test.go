package ai

import "testing"

func TestArticleAnchorID(t *testing.T) {
	cases := []struct {
		index int
		want  string
	}{
		{0, "article-section-000"},
		{1, "article-section-001"},
		{12, "article-section-012"},
		{999, "article-section-999"},
		{1000, "article-section-1000"},
	}
	for _, tc := range cases {
		if got := articleAnchorID(tc.index); got != tc.want {
			t.Errorf("articleAnchorID(%d) = %q, want %q", tc.index, got, tc.want)
		}
	}
}

func TestAnnotateArticleForSummary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "heading multiline paragraph inline link list and fenced code",
			in:   "# Title\n\nA paragraph\nthat continues with [a link](https://example.com).\n\n- first item\n  continuation\n- ![only image](https://example.com/image.png)\n\n```go\nfmt.Println(\"not an article block\")\n```\n\n## End\n",
			want: "[正文锚点: article-section-001]\n# Title\n\n[正文锚点: article-section-002]\nA paragraph\nthat continues with [a link](https://example.com).\n\n[正文锚点: article-section-003]\n- first item\n  continuation\n- ![only image](https://example.com/image.png)\n\n```go\nfmt.Println(\"not an article block\")\n```\n\n[正文锚点: article-section-004]\n## End\n",
		},
		{
			name: "blank content",
			in:   "\n  \n\t\n",
			want: "\n  \n\t\n",
		},
		{
			name: "image only block",
			in:   "![cover](https://example.com/cover.png)\n\n![second](https://example.com/second.png)\n",
			want: "![cover](https://example.com/cover.png)\n\n![second](https://example.com/second.png)\n",
		},
		{
			name: "link only paragraph is addressable",
			in:   "[Read the report](https://example.com/report)\n",
			want: "[正文锚点: article-section-001]\n[Read the report](https://example.com/report)\n",
		},
		{
			name: "ordered and nested lists are deterministic",
			in:   "1. first\n   1. nested one\n   2. nested two\n2. second\n   - nested bullet\n",
			want: "[正文锚点: article-section-001]\n1. first\n[正文锚点: article-section-002]\n   1. nested one\n[正文锚点: article-section-003]\n   2. nested two\n[正文锚点: article-section-004]\n2. second\n[正文锚点: article-section-005]\n   - nested bullet\n",
		},
		{
			name: "blockquote",
			in:   "> quoted text\n> continued quote\n\noutside\n",
			want: "[正文锚点: article-section-001]\n> quoted text\n> continued quote\n\n[正文锚点: article-section-002]\noutside\n",
		},
		{
			name: "tilde fence",
			in:   "~~~markdown\n# not a heading\n~~~\n\nAfter fence\n",
			want: "~~~markdown\n# not a heading\n~~~\n\n[正文锚点: article-section-001]\nAfter fence\n",
		},
		{
			name: "thematic separators",
			in:   "---\n\n***\n\n___\n\nText\n",
			want: "---\n\n***\n\n___\n\n[正文锚点: article-section-001]\nText\n",
		},
		{
			name: "preserves CRLF",
			in:   "# Title\r\n\r\nParagraph\r\ncontinued\r\n",
			want: "[正文锚点: article-section-001]\r\n# Title\r\n\r\n[正文锚点: article-section-002]\r\nParagraph\r\ncontinued\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := annotateArticleForSummary(tc.in); got != tc.want {
				t.Errorf("annotateArticleForSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHasAddressableArticleBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"blank", "\n \t\n", false},
		{"image only", "![image](https://example.com/image.png)\n", false},
		{"fenced code only", "```\ntext\n```\n", false},
		{"thematic separator only", "---\n", false},
		{"link paragraph", "[Read](https://example.com)\n", true},
		{"blockquote", "> quote\n", true},
		{"list item", "- item\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasAddressableArticleBlock(tc.in); got != tc.want {
				t.Errorf("hasAddressableArticleBlock() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnnotateArticleForSummaryIgnoresFenceWithTrailingText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "backtick fence",
			in:   "```\ninside\n``` trailing\nstill inside\n```\n\nAfter\n",
			want: "```\ninside\n``` trailing\nstill inside\n```\n\n[正文锚点: article-section-001]\nAfter\n",
		},
		{
			name: "tilde fence",
			in:   "~~~\ninside\n~~~ trailing\nstill inside\n~~~\n\nAfter\n",
			want: "~~~\ninside\n~~~ trailing\nstill inside\n~~~\n\n[正文锚点: article-section-001]\nAfter\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := annotateArticleForSummary(tc.in); got != tc.want {
				t.Errorf("annotateArticleForSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAnnotateArticleForSummaryDoesNotSuppressMeaningfulBlockquote(t *testing.T) {
	in := "> ![](https://example.com/image.png)\n> meaningful quote\n"
	want := "> ![](https://example.com/image.png)\n[正文锚点: article-section-001]\n> meaningful quote\n"
	if got := annotateArticleForSummary(in); got != want {
		t.Errorf("annotateArticleForSummary() = %q, want %q", got, want)
	}
}

func TestAnnotateArticleForSummaryStartsIDsAtOne(t *testing.T) {
	if got := annotateArticleForSummary("Text\n"); got != "[正文锚点: article-section-001]\nText\n" {
		t.Errorf("annotateArticleForSummary() = %q, want first ID article-section-001", got)
	}
}

func TestAnnotateArticleForSummaryKeepsImageOnlyMiddleQuoteInBlock(t *testing.T) {
	in := "> text one\n> ![](https://example.com/image.png)\n> text two\n"
	want := "> text one\n> ![](https://example.com/image.png)\n> text two\n"
	if got := annotateArticleForSummary(in); got != "[正文锚点: article-section-001]\n"+want {
		t.Errorf("annotateArticleForSummary() = %q, want one anchor for continuous quote", got)
	}
}

func TestAnnotateArticleForSummaryAnchorsParagraphWithImageContinuation(t *testing.T) {
	in := "![cover](https://example.com/cover.png)\n说明文字\n"
	want := "[正文锚点: article-section-001]\n" + in
	if got := annotateArticleForSummary(in); got != want {
		t.Errorf("annotateArticleForSummary() = %q, want image-led paragraph anchor", got)
	}
}

func TestHasAddressableArticleBlockForImageContinuation(t *testing.T) {
	if !hasAddressableArticleBlock("![cover](https://example.com/cover.png)\n说明文字\n") {
		t.Fatal("hasAddressableArticleBlock() = false, want true for meaningful continuation")
	}
}

func TestAnnotateArticleForSummarySkipsIndentedCodeAndListContinuations(t *testing.T) {
	in := "    top-level code\n\tmore top-level code\n\n- item\n\n  continuation paragraph\n\noutside\n"
	want := "    top-level code\n\tmore top-level code\n\n[正文锚点: article-section-001]\n- item\n\n  continuation paragraph\n\n[正文锚点: article-section-002]\noutside\n"
	if got := annotateArticleForSummary(in); got != want {
		t.Errorf("annotateArticleForSummary() = %q, want %q", got, want)
	}
}
