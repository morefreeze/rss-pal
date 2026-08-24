package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const articleAnchorTestContent = "# 第一节\n\n第一段内容。\n\n第二段内容。\n"

func TestBuildDetailedArticlePromptInputAnnotatesAddressableContentAndExplainsLinkRules(t *testing.T) {
	annotated, instruction := buildDetailedArticlePromptInput(articleAnchorTestContent)
	if !strings.Contains(annotated, "[正文锚点: article-section-001]") ||
		!strings.Contains(annotated, "[正文锚点: article-section-003]") {
		t.Fatalf("annotated content missing canonical markers:\n%s", annotated)
	}
	for _, want := range []string{
		"原文顺序",
		"按大意合并相邻内容",
		"[查看原文](#article-section-NNN)",
		"来自正文中已有的锚点",
		"至多一个",
		"不要每段都添加跳转",
		"短文或整篇只讲一件事时可以完全不添加",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("instruction missing %q:\n%s", want, instruction)
		}
	}
}

func TestDetailedArticleAnchorInstructionRequiresBoundedLinks(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{name: "strict markdown format", want: []string{"[查看原文](#article-section-NNN)", "示例：[查看原文](#article-section-003)"}},
		{name: "bounded thresholds", want: []string{"至少 3 个", "至多 30 个"}},
		{name: "three available anchors precondition", want: []string{"至少 3 个可用且不重复的正文锚点", "多个清晰的章节或主题组"}},
		{name: "distinct anchors map to meaningful groups", want: []string{"多个清晰的章节或主题组", "每个语义组都能对应不同的正文锚点"}},
		{name: "two high-level topics still qualify", want: []string{"即使只有两个高层主题，只要正文有至少 3 个可用锚点也必须添加链接"}},
		{name: "eligible articles must link", want: []string{"必须添加至少 3 个、至多 30 个"}},
		{name: "fewer than three means zero", want: []string{"可用锚点少于 3 个", "输出 0 个链接"}},
		{name: "no duplicate ids", want: []string{"不得重复使用同一个锚点"}},
		{name: "only supplied ids", want: []string{"只能使用正文中提供的锚点"}},
		{name: "single theme exception", want: []string{"短文或单一连续主题的文章可以不添加链接"}},
		{name: "zero only for exceptions", want: []string{"只有可用锚点少于 3 个，或短文/单一连续主题时，才允许输出 0 个链接"}},
		{name: "never one or two", want: []string{"不得只添加 1 或 2 个链接"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.want {
				if !strings.Contains(detailedArticleAnchorInstruction, want) {
					t.Errorf("instruction missing %q:\n%s", want, detailedArticleAnchorInstruction)
				}
			}
		})
	}
}

func TestBuildDetailedArticlePromptInputLeavesImageOnlyContentUnchanged(t *testing.T) {
	content := "![cover](https://example.com/cover.jpg)\n"
	annotated, instruction := buildDetailedArticlePromptInput(content)
	if annotated != content {
		t.Errorf("annotated = %q, want original %q", annotated, content)
	}
	if instruction != "" {
		t.Errorf("instruction = %q, want empty", instruction)
	}
}

func TestSummarizeDetailedPromptLeavesEmptyAndImageOnlyContentUnannotated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "image only", content: "![cover](https://example.com/cover.jpg)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, cap := newCaptureServer(t, "brief", "detailed")
			s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

			result, err := s.Summarize(context.Background(), "Title", tc.content)
			if err != nil {
				t.Fatalf("Summarize: %v", err)
			}
			if result.Brief != "brief" || result.Detailed != "detailed" {
				t.Fatalf("summary = %#v, want normal brief and detailed responses", result)
			}
			if len(cap.bodies) != 2 {
				t.Fatalf("captured %d requests, want 2", len(cap.bodies))
			}
			assertTextPromptWithoutArticleAnchors(t, requestUserText(t, cap.bodies[1]), "default detailed "+tc.name)
		})
	}
}

func TestDetailedPromptsUseAnchorsButBriefPromptsDoNot(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief", "detailed", "brief template", "detailed template")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

	if _, err := s.Summarize(context.Background(), "Title", articleAnchorTestContent); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if _, err := s.SummarizeWithTemplate(
		context.Background(), "Title", articleAnchorTestContent,
		"BRIEF TEMPLATE\n{title}\n{content}",
		"DETAILED TEMPLATE\n{title}\n{content}",
	); err != nil {
		t.Fatalf("SummarizeWithTemplate: %v", err)
	}
	if len(cap.bodies) != 4 {
		t.Fatalf("captured %d requests, want 4", len(cap.bodies))
	}

	assertTextPromptWithoutArticleAnchors(t, requestUserText(t, cap.bodies[0]), "default brief")
	assertTextPromptWithArticleAnchors(t, requestUserText(t, cap.bodies[1]), "default detailed")
	assertTextPromptWithoutArticleAnchors(t, requestUserText(t, cap.bodies[2]), "template brief")
	detailedTemplate := requestUserText(t, cap.bodies[3])
	assertTextPromptWithArticleAnchors(t, detailedTemplate, "template detailed")
	if !strings.Contains(detailedTemplate, "DETAILED TEMPLATE") ||
		!strings.HasSuffix(detailedTemplate, detailedArticleAnchorInstruction) {
		t.Errorf("custom detailed template should precede the system-owned instruction:\n%s", detailedTemplate)
	}
}

func TestSummarizeWithTemplateDoesNotAddAnchorsWhenDetailedTemplateOmitsContent(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief", "detailed")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

	if _, err := s.SummarizeWithTemplate(
		context.Background(), "Title", articleAnchorTestContent,
		"BRIEF {content}", "DETAILED ONLY {title}",
	); err != nil {
		t.Fatalf("SummarizeWithTemplate: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("captured %d requests, want 2", len(cap.bodies))
	}
	detailedPrompt := requestUserText(t, cap.bodies[1])
	if detailedPrompt != "DETAILED ONLY Title" {
		t.Errorf("detailed prompt = %q, want rendered content-less template unchanged", detailedPrompt)
	}
	assertTextPromptWithoutArticleAnchors(t, detailedPrompt, "content-less template detailed")
}

func TestGenerateDetailedAnnotatesOnlyTruncatedContent(t *testing.T) {
	srv, cap := newCaptureServer(t, "detailed")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	content := strings.Repeat("前", maxContentRunes) + "\n\n不应进入提示词的正文"

	if _, err := s.generateDetailed(context.Background(), "Title", content); err != nil {
		t.Fatalf("generateDetailed: %v", err)
	}
	if len(cap.bodies) != 1 {
		t.Fatalf("captured %d requests, want 1", len(cap.bodies))
	}
	prompt := requestUserText(t, cap.bodies[0])
	if strings.Contains(prompt, "不应进入提示词的正文") {
		t.Errorf("detailed prompt includes content beyond truncation budget:\n%s", prompt)
	}
	assertTextPromptWithArticleAnchors(t, prompt, "truncated detailed")
}

func assertTextPromptWithArticleAnchors(t *testing.T, prompt, path string) {
	t.Helper()
	if !strings.Contains(prompt, "[正文锚点: article-section-001]") {
		t.Errorf("%s prompt has no article marker:\n%s", path, prompt)
	}
	if !strings.Contains(prompt, detailedArticleAnchorInstruction) {
		t.Errorf("%s prompt has no article-anchor instruction:\n%s", path, prompt)
	}
}

func assertTextPromptWithoutArticleAnchors(t *testing.T, prompt, path string) {
	t.Helper()
	if strings.Contains(prompt, "[正文锚点:") {
		t.Errorf("%s prompt unexpectedly has article marker:\n%s", path, prompt)
	}
	if strings.Contains(prompt, detailedArticleAnchorInstruction) {
		t.Errorf("%s prompt unexpectedly has article-anchor instruction:\n%s", path, prompt)
	}
}

func requestUserText(t *testing.T, body []byte) string {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	for _, message := range request.Messages {
		if message.Role == "user" {
			return message.Content
		}
	}
	t.Fatal("request has no user message")
	return ""
}
