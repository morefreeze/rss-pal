package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func summaryRequestMessages(t *testing.T, body []byte) (system, user string) {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	for _, msg := range req.Messages {
		var text string
		if err := json.Unmarshal(msg.Content, &text); err != nil {
			var blocks []contentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err != nil {
				t.Fatal(err)
			}
			for _, block := range blocks {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
		if msg.Role == "system" {
			system = text
		}
		if msg.Role == "user" {
			user = text
		}
	}
	return
}

func captureDefaultSummaryRequests(t *testing.T, content, briefReply string, stream, vision bool) ([][]byte, string) {
	t.Helper()
	var url string
	var capturedBodies func() [][]byte
	if stream {
		srv, cap := newStreamCaptureServer(t, briefReply, "details")
		url = srv.URL
		capturedBodies = func() [][]byte { return cap.bodies }
	} else {
		srv, cap := newCaptureServer(t, briefReply, "details")
		url = srv.URL
		capturedBodies = func() [][]byte { return cap.bodies }
	}
	s := NewSummarizerWithModel("test-key", url, "test-model")
	s.SetVisionModel("vision-model")
	var paths, imageURLs []string
	if vision {
		path := filepath.Join(t.TempDir(), "test.jpg")
		writeTestJPEG(t, path)
		paths = []string{path}
		imageURLs = []string{"https://example.com/chart.jpg"}
	}
	var result *SummaryResult
	var err error
	switch {
	case stream && vision:
		result, err = s.SummarizeWithImagesStream(context.Background(), "Title", content, paths, imageURLs, func(string) {}, func(string) {})
	case stream:
		result, err = s.SummarizeStream(context.Background(), "Title", content, func(string) {}, func(string) {})
	case vision:
		result, err = s.SummarizeWithImages(context.Background(), "Title", content, paths, imageURLs)
	default:
		result, err = s.Summarize(context.Background(), "Title", content)
	}
	if err != nil {
		t.Fatal(err)
	}
	bodies := capturedBodies()
	if len(bodies) != 2 {
		t.Fatalf("requests=%d, want one brief and one detailed request", len(bodies))
	}
	return bodies, result.Brief
}

func TestDefaultSummaryQualityReachesEveryTransport(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stream, vision bool
	}{
		{"text", false, false}, {"stream", true, false},
		{"vision", false, true}, {"vision stream", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := strings.Repeat("文", 1000)
			if tc.vision {
				content = "图表"
			}
			bodies, brief := captureDefaultSummaryRequests(t, content, "已有要点：保留作者的验证前提", tc.stream, tc.vision)
			for i, body := range bodies {
				system, user := summaryRequestMessages(t, body)
				for _, rule := range []string{"来源性质", "公开分享", "事实与推断", "条件与对应结论一起呈现", "条件步骤", "何时触发、何时跳过", "不能只写“条件性”或“可选”", "例外", "完整评测", "实验分组"} {
					if !strings.Contains(system, rule) {
						t.Errorf("request %d missing factual guard %q", i, rule)
					}
				}
				want := "总字符数不超过100"
				if i == 1 {
					want = "总字符数不超过320"
				}
				if tc.vision {
					if !strings.Contains(user, "https://example.com/chart.jpg") {
						t.Errorf("vision request %d lost image reference URL", i)
					}
					want = "总字符数不超过300"
					if i == 1 {
						want = "总字符数不超过2000"
					}
				}
				if !strings.Contains(user, want) {
					t.Errorf("request %d missing adaptive limit %s", i, want)
				}
				if !strings.Contains(user, "宁可舍弃次要要点，也不能删掉改变结论含义的条件") {
					t.Errorf("request %d allows compression to strip a material condition", i)
				}
				if !strings.Contains(user, "触发条件和跳过条件优先于操作参数") {
					t.Errorf("request %d does not prioritize actionable workflow conditions", i)
				}
				if i == 1 {
					for _, rule := range []string{brief, "不重复", "关键前提", "要点漏掉的必要条件或例外仍须补全"} {
						if !strings.Contains(user, rule) {
							t.Errorf("detailed request missing %q", rule)
						}
					}
				}
			}
		})
	}
}

func assertSummaryClaimBoundaryGuidance(t *testing.T, system string) {
	t.Helper()
	for _, rule := range []string{
		"实际测量对象、指标及其对应结果和比较范围",
		"不能把下游结果倒推或转移成上游对象的质量结论",
		"分歧、误差或不一致",
		"可能不等于必然",
		"重要性降低或作用变化不等于消失",
		"观察结果、预测与建议不得互换",
		"分别保留各自的主体、谓词和限定",
		"要点与详细总结对同一主张的对象、范围、确定性和程度必须一致",
		"并以原文为准",
	} {
		if !strings.Contains(system, rule) {
			t.Errorf("missing claim-boundary guard %q", rule)
		}
	}
}

func TestSummaryClaimBoundariesReachOutboundRequests(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stream, vision bool
	}{
		{"text", false, false}, {"stream", true, false},
		{"vision", false, true}, {"vision stream", true, true},
	} {
		for _, article := range []struct {
			name, content, misleadingBrief string
		}{
			{
				"measured object",
				"Although GPT-3.5 and human annotators disagree on nearly half the samples, classifiers trained with their respective annotations achieve comparable accuracy.",
				"GPT-3.5 与人工标注者的准确率相当。",
			},
			{
				"modality and distinct claims",
				"Unit tests may disappear. UI becomes less important as interfaces can be generated in real time.",
				"单元测试和界面都将消失。",
			},
		} {
			t.Run(tc.name+"/"+article.name, func(t *testing.T) {
				bodies, brief := captureDefaultSummaryRequests(t, article.content, article.misleadingBrief, tc.stream, tc.vision)
				if brief != article.misleadingBrief {
					t.Fatalf("generated brief=%q, want the misleading fixture %q", brief, article.misleadingBrief)
				}
				for i, body := range bodies {
					system, user := summaryRequestMessages(t, body)
					assertSummaryClaimBoundaryGuidance(t, system)
					if !strings.Contains(user, article.content) {
						t.Errorf("request %d changed or dropped the source comparison or qualifications", i)
					}
					var req struct {
						Model     string `json:"model"`
						MaxTokens int    `json:"max_tokens"`
					}
					if err := json.Unmarshal(body, &req); err != nil {
						t.Fatal(err)
					}
					wantModel, wantTokens := "test-model", briefMaxTokens
					if tc.vision {
						wantModel = "vision-model"
					}
					if i == 1 {
						wantTokens = detailedMaxTokens
						if !strings.Contains(user, "已生成的前置要点（仅供去重，不是新的事实来源；其中原文无依据的表述不要沿用）：\n"+brief) {
							t.Error("detailed request must treat the generated brief only as untrusted deduplication context")
						}
					}
					if req.Model != wantModel || req.MaxTokens != wantTokens {
						t.Errorf("request %d changed model/token budget: %s/%d, want %s/%d", i, req.Model, req.MaxTokens, wantModel, wantTokens)
					}
				}
			})
		}
	}
}

func TestLocalSummaryClaimBoundariesUseTheSameSourceGuard(t *testing.T) {
	const content = "标注虽有分歧，分别用各自标注训练的分类器准确率相当。单元测试可能消失；界面重要性降低，并可实时生成。"
	for _, custom := range []bool{false, true} {
		name, briefTemplate, detailedTemplate := "default", "", ""
		if custom {
			name, briefTemplate, detailedTemplate = "custom", "BRIEF {content}", "DETAIL {content}"
		}
		t.Run(name, func(t *testing.T) {
			brief, detailed := LocalSummaryPrompts("Title", content, briefTemplate, detailedTemplate)
			for i, prompt := range []string{brief, detailed} {
				system, user, ok := strings.Cut(prompt, "\n\n")
				if !ok {
					t.Fatalf("prompt %d has no grounding guard", i)
				}
				assertSummaryClaimBoundaryGuidance(t, system)
				if !strings.Contains(user, content) {
					t.Errorf("prompt %d changed or dropped the source comparison or qualifications", i)
				}
				if strings.Contains(user, "已生成的前置要点") {
					t.Errorf("prompt %d unexpectedly introduced a prior summary as source context", i)
				}
			}
		})
	}
}

func TestDefaultSummaryLimitsIgnorePresentationMarkup(t *testing.T) {
	plain := strings.Repeat("文", 1000)
	for _, content := range []string{
		plain,
		"[正文锚点: article-section-001]\n" + plain,
		plain + "\n![cover](https://example.com/" + strings.Repeat("x", 5000) + ")",
		"[" + plain + "](https://example.com/" + strings.Repeat("x", 5000) + ")",
		strings.Repeat("文😀\u3000\n", 500),
	} {
		if got := defaultSummaryLimit(content, false); got != 100 {
			t.Errorf("brief limit=%d, want 100", got)
		}
		if got := defaultSummaryLimit(content, true); got != 320 {
			t.Errorf("detailed limit=%d, want 320", got)
		}
	}
	for _, tc := range []struct {
		content         string
		brief, detailed int
	}{
		{"", 300, 2000},
		{"![cover](https://example.com/image.jpg)", 300, 2000},
		{"短文", 16, 48},
		{strings.Repeat("文", 10000), 300, 2000},
	} {
		if got := defaultSummaryLimit(tc.content, false); got != tc.brief {
			t.Errorf("brief limit=%d, want %d", got, tc.brief)
		}
		if got := defaultSummaryLimit(tc.content, true); got != tc.detailed {
			t.Errorf("detailed limit=%d, want %d", got, tc.detailed)
		}
	}
}

func TestCustomSummaryKeepsItsBudgetAndExplicitSourceEvidence(t *testing.T) {
	content := "作者明确称这份材料是泄露的，并非仅仅公开分享。"
	srv, cap := newCaptureServer(t, "brief", "detailed")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	if _, err := s.SummarizeWithTemplate(context.Background(), "Title", content, "BRIEF {content}", "DEEP ANALYSIS {content}"); err != nil {
		t.Fatal(err)
	}
	for i, body := range cap.bodies {
		system, user := summaryRequestMessages(t, body)
		if !strings.Contains(system, "原文明示这些性质时可忠实转述") {
			t.Fatal("guardrail blanket-bans legitimate source statements")
		}
		if !strings.Contains(user, content) {
			t.Fatal("explicit source evidence was rewritten")
		}
		if strings.Contains(user, "默认总结要求") || strings.Contains(user, "已生成的前置要点") {
			t.Fatal("default-only compression leaked into a custom template")
		}
		limit := "总字符数不超过300"
		if i == 1 {
			limit = "总字符数不超过2000"
		}
		if !strings.Contains(user, limit) {
			t.Fatalf("custom request %d lost its existing budget", i)
		}
	}
}
