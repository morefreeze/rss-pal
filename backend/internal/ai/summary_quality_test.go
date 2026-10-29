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
		"统计数字必须绑定完整事件",
		"包括“且”“或”等逻辑关系",
		"分母、样本总体和比较范围",
		"不能把复合事件的频率改成其中单项的频率",
		"不同实验条件下的指标不能混用",
		"否定词及“所有”“任一”“部分”等量词的作用域和先后关系",
		"“没有一个 A 在所有 B 上满足条件”不等于“所有 A 在所有 B 上都不满足条件”",
		"必要时舍弃数字，不能删去限定来强化结论",
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
			{
				"compound event metric",
				"After the curriculum, the model overwrote the reward and avoided detection in fewer than 1 in 1,000 trials. Under curricula directly incentivizing reward hacking, reward overwriting occurred in fewer than 1% of trials, and unit-test hacking even less often.",
				"完成课程后，模型重写奖励的发生率低于 1/1000。",
			},
			{
				"disjunction denominator",
				"Among 400 evaluation runs, 12% met the failure criterion: condition A or condition B occurred. The comparison group used the same combined criterion and population.",
				"条件 A 在 12% 的运行中出现。",
			},
			{
				"quantifier negation scope",
				"Different detectors perform better on different tasks. None of the tested classifiers achieved AUROC greater than 60% across all tested RL environments.",
				"所有测试分类器在所有环境中的 AUROC 都不超过 60%。",
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

func assertDefaultDetailedMechanismGuidance(t *testing.T, prompt string) {
	t.Helper()
	for _, rule := range []string{
		"选入总结的关键方法或框架",
		"简要解释作用机制及原文给出的主要限制，不只列名称",
		"机制、条件和限制优先于次要示例和重复定义",
		"按现有篇幅取舍，不必罗列每种方法",
	} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("detailed prompt missing selected-method guidance %q", rule)
		}
	}
}

func TestDefaultDetailedSummaryExplainsSelectedMethods(t *testing.T) {
	const content = "Decoupled approval samples the feedback query independently from the action taken in the world. Detection compares trusted and target policy action distributions, but different detectors work better on different tasks. The framework distinguishes intended target features from unintended spoiler features; its robustness metric is statistically significant only for the sentiment feature."
	for _, tc := range []struct {
		name           string
		stream, vision bool
	}{
		{"text", false, false}, {"stream", true, false},
		{"vision", false, true}, {"vision stream", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies, brief := captureDefaultSummaryRequests(t, content, "这些方法已解决所有风险。", tc.stream, tc.vision)
			for i, body := range bodies {
				_, user := summaryRequestMessages(t, body)
				if !strings.Contains(user, content) {
					t.Errorf("request %d dropped the method mechanisms or limitations", i)
				}
				if i == 0 {
					if strings.Contains(user, "选入总结的关键方法或框架") {
						t.Error("detailed-only method explanation leaked into the brief request")
					}
					continue
				}
				assertDefaultDetailedMechanismGuidance(t, user)
				if !strings.Contains(user, "已生成的前置要点（仅供去重，不是新的事实来源；其中原文无依据的表述不要沿用）：\n"+brief) {
					t.Error("detailed method explanation must remain grounded in the article rather than the generated brief")
				}
			}
		})
	}
}

func assertDefaultStatisticalUserGuidance(t *testing.T, prompt, content string, detailed bool) {
	t.Helper()
	rules := []string{
		"统计范围复核：",
		"要点与详细总结的事件和比较范围必须一致，以原文为准",
		"原文只是否定单一方法跨所有条件的一致达标时",
		"不能写成所有方法在所有条件下都失败",
		"不要用“均未超过”等暗示普遍上限的写法",
	}
	unexpected := "多重否定或嵌套量词的结论"
	if detailed {
		rules = append(rules,
			"多重否定或嵌套量词的结论",
			"仅当原文明确说明没有单一方法在所有条件下都达标时，才可用“没有任何单一方法能在每一种条件下都达到阈值”",
			"其他量词组合必须保留原文的主体、否定和作用域，不得套用该否定句式",
			"篇幅不足以保留完整限定时，省略次要数字",
		)
		unexpected = "要点中的统计取舍："
	} else {
		rules = append(rules,
			"要点中的统计取舍：",
			"跨多个环境或分组以及复合事件的统计，优先定性概括",
			"把次要数字及完整限定留给详细总结",
			"单一明确的核心数字可保留",
		)
	}
	for _, rule := range rules {
		if !strings.Contains(prompt, rule) {
			t.Errorf("user prompt missing statistical scope guidance %q", rule)
		}
	}
	if strings.Contains(prompt, unexpected) {
		t.Errorf("user prompt received the other summary stage's guidance %q", unexpected)
	}
	if strings.Contains(prompt, "多重否定或嵌套量词的结论，优先用") {
		t.Error("user guidance must not prescribe the negative template for every nested quantifier conclusion")
	}
	sourceAt, guidanceAt := strings.Index(prompt, content), strings.Index(prompt, "统计范围复核：")
	if sourceAt < 0 || guidanceAt < sourceAt+len(content) {
		t.Error("statistical scope guidance must follow the intact article in the user prompt")
	}
}

func assertNoDefaultStatisticalUserGuidance(t *testing.T, prompt string) {
	t.Helper()
	for _, marker := range []string{"统计范围复核：", "要点中的统计取舍：", "多重否定或嵌套量词的结论"} {
		if strings.Contains(prompt, marker) {
			t.Errorf("custom template unexpectedly received default statistical guidance %q", marker)
		}
	}
}

func TestDefaultStatisticalGuidanceFollowsSourceInOutboundRequests(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stream, vision bool
	}{
		{"text", false, false}, {"stream", true, false},
		{"vision", false, true}, {"vision stream", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, article := range []struct{ name, content string }{
				{"negative universal", "The main trial enrolled 120 participants. Different methods worked better under different conditions; no single method met the threshold in every condition. In 600 runs, the device both reset and avoided an alarm in fewer than 1% of runs."},
				{"universal existential", "Every method met the threshold in at least one condition."},
				{"existential universal", "One method met the threshold in every condition."},
			} {
				t.Run(article.name, func(t *testing.T) {
					bodies, brief := captureDefaultSummaryRequests(t, article.content, "所有方法都未达标。", tc.stream, tc.vision)
					for i, body := range bodies {
						system, user := summaryRequestMessages(t, body)
						assertSummaryClaimBoundaryGuidance(t, system)
						assertDefaultStatisticalUserGuidance(t, user, article.content, i == 1)
						if i == 1 && !strings.Contains(user, "已生成的前置要点（仅供去重，不是新的事实来源；其中原文无依据的表述不要沿用）：\n"+brief) {
							t.Error("detailed scope guidance must not make the misleading brief authoritative")
						}
					}
				})
			}
		})
	}
}

func TestLocalSummaryPreservesPositiveQuantifierSources(t *testing.T) {
	for _, content := range []string{
		"每个方法都至少在一种条件下达标。",
		"有一个方法在所有条件下都达标。",
	} {
		t.Run(content, func(t *testing.T) {
			brief, detailed := LocalSummaryPrompts("Title", content, "", "")
			for i, prompt := range []string{brief, detailed} {
				_, user, _ := strings.Cut(prompt, "\n\n")
				assertDefaultStatisticalUserGuidance(t, user, content, i == 1)
			}
		})
	}
}

func TestLocalSummaryClaimBoundariesUseTheSameSourceGuard(t *testing.T) {
	const content = "标注虽有分歧，分别用各自标注训练的分类器准确率相当。单元测试可能消失；界面重要性降低，并可实时生成。完成课程后，模型重写奖励且规避检测的频率低于千分之一；另一类课程下仅重写奖励的频率低于百分之一。不同检测器各有擅长任务，没有任何一个在全部环境中的 AUROC 都超过六成。检测方法比较可信策略与目标策略的动作分布，在不同任务上效果不同。"
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
				if custom {
					assertNoDefaultStatisticalUserGuidance(t, user)
				} else {
					assertDefaultStatisticalUserGuidance(t, user, content, i == 1)
				}
				if !custom && i == 1 {
					assertDefaultDetailedMechanismGuidance(t, user)
				} else if strings.Contains(user, "选入总结的关键方法或框架") {
					t.Errorf("prompt %d unexpectedly received default detailed-only guidance", i)
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
	for _, stream := range []bool{false, true} {
		name := "text"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var bodies [][]byte
			if stream {
				srv, cap := newStreamCaptureServer(t, "brief", "detailed")
				s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
				if _, err := s.SummarizeWithTemplateStream(context.Background(), "Title", content, "BRIEF {content}", "DEEP ANALYSIS {content}", func(string) {}, func(string) {}); err != nil {
					t.Fatal(err)
				}
				bodies = cap.bodies
			} else {
				srv, cap := newCaptureServer(t, "brief", "detailed")
				s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
				if _, err := s.SummarizeWithTemplate(context.Background(), "Title", content, "BRIEF {content}", "DEEP ANALYSIS {content}"); err != nil {
					t.Fatal(err)
				}
				bodies = cap.bodies
			}
			if len(bodies) != 2 {
				t.Fatalf("custom requests=%d, want 2", len(bodies))
			}
			for i, body := range bodies {
				system, user := summaryRequestMessages(t, body)
				assertSummaryClaimBoundaryGuidance(t, system)
				assertNoDefaultStatisticalUserGuidance(t, user)
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
		})
	}
}
