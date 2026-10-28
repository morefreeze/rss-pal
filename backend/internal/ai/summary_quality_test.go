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

func TestDefaultSummaryQualityReachesEveryTransport(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stream, vision bool
	}{
		{"text", false, false}, {"stream", true, false},
		{"vision", false, true}, {"vision stream", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brief := "已有要点：保留作者的验证前提"
			var url string
			var capturedBodies func() [][]byte
			if tc.stream {
				srv, cap := newStreamCaptureServer(t)
				url = srv.URL
				capturedBodies = func() [][]byte { return cap.bodies }
				brief = "ok"
			} else {
				srv, cap := newCaptureServer(t, brief, "details")
				url = srv.URL
				capturedBodies = func() [][]byte { return cap.bodies }
			}
			s := NewSummarizerWithModel("test-key", url, "test-model")
			s.SetVisionModel("vision-model")
			var paths []string
			var imageURLs []string
			if tc.vision {
				path := filepath.Join(t.TempDir(), "test.jpg")
				writeTestJPEG(t, path)
				paths = []string{path}
				imageURLs = []string{"https://example.com/chart.jpg"}
			}
			var err error
			content := strings.Repeat("文", 1000)
			if tc.vision {
				content = "图表"
			}
			switch {
			case tc.stream && tc.vision:
				_, err = s.SummarizeWithImagesStream(context.Background(), "共享文件，不声称泄露", content, paths, imageURLs, func(string) {}, func(string) {})
			case tc.stream:
				_, err = s.SummarizeStream(context.Background(), "共享文件，不声称泄露", content, func(string) {}, func(string) {})
			case tc.vision:
				_, err = s.SummarizeWithImages(context.Background(), "共享文件，不声称泄露", content, paths, imageURLs)
			default:
				_, err = s.Summarize(context.Background(), "共享文件，不声称泄露", content)
			}
			if err != nil {
				t.Fatal(err)
			}
			bodies := capturedBodies()
			if len(bodies) != 2 {
				t.Fatalf("requests=%d", len(bodies))
			}
			for i, body := range bodies {
				system, user := summaryRequestMessages(t, body)
				for _, rule := range []string{"来源性质", "公开分享", "事实与推断", "条件与对应结论一起呈现", "条件步骤", "例外", "完整评测", "实验分组"} {
					if !strings.Contains(system, rule) {
						t.Errorf("request %d missing factual guard %q", i, rule)
					}
				}
				want := "总字符数不超过100"
				if i == 1 {
					want = "总字符数不超过320"
				}
				if tc.vision {
					if !strings.Contains(user, imageURLs[0]) {
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
