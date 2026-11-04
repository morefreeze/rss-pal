package summaryeval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/ai"
)

func testConfig() Config {
	return Config{Live: true, MaxCalls: 32, Routes: []Route{Default}, CaseIDs: []string{"017"}, Generator: Provider{Key: "test-secret", BaseURL: "https://provider.example/v1", Model: "test-model"}}
}

func scriptedProvider(t *testing.T, suite Suite, lenient bool, unknown bool) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req struct {
			MaxTokens int  `json:"max_tokens"`
			Stream    bool `json:"stream"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		var content string
		json.Unmarshal(req.Messages[len(req.Messages)-1].Content, &content)
		var answer string
		if _, data, ok := strings.Cut(content, "INPUT:\n"); ok {
			var in struct {
				Title  string `json:"title"`
				Output Output `json:"candidate_summary"`
			}
			if err := json.Unmarshal([]byte(data), &in); err != nil {
				t.Fatal(err)
			}
			var c Case
			for _, candidate := range suite.Cases {
				if candidate.Title == in.Title {
					c = candidate
				}
			}
			verdict := Pass
			if !lenient {
				for _, ctrl := range suite.Controls {
					if ctrl.Output == in.Output && ctrl.Expected == Fail {
						verdict = Fail
					}
				}
			}
			if unknown && in.Output == suite.Controls[0].Output && len(content) > 0 {
				verdict = Unknown
			}
			j := testJudgment(c, in.Output, verdict)
			b, _ := json.Marshal(j)
			answer = string(b)
		} else {
			if strings.Contains(content, "RUBRIC_ONLY_NEVER_GENERATE") {
				t.Error("semantic rubric leaked into generator")
			}
			answer = suite.Controls[0].Output.Detailed
			if req.MaxTokens == 1500 {
				answer = suite.Controls[0].Output.Brief
			}
		}
		b, _ := json.Marshal(answer)
		if req.Stream {
			rsp := wireResponse(fmt.Sprintf("data: {\"model\":\"reported-model\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n", b))
			rsp.Header.Set("Content-Type", "text/event-stream")
			return rsp, nil
		}
		return wireResponse(fmt.Sprintf(`{"model":"reported-model","choices":[{"message":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, b)), nil
	})
}

func TestRunRefusesUnoptedNetworkAndInvalidConfiguration(t *testing.T) {
	suite, _ := LoadSuite()
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Live = false },
		func(c *Config) { c.MaxCalls = 0 },
		func(c *Config) { c.MaxCalls = 257 },
		func(c *Config) { c.Generator.Key = "" },
		func(c *Config) { c.Generator.BaseURL = "https://user:secret@provider.example/path?key=secret" },
		func(c *Config) { c.Generator.Model = "model\nprivate" },
		func(c *Config) { c.Routes = []Route{"unknown"} },
		func(c *Config) { c.CaseIDs = []string{"missing"} },
		func(c *Config) { c.Routes = []Route{Vision} },
	} {
		cfg := testConfig()
		mutate(&cfg)
		if _, err := Run(context.Background(), cfg, suite, roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid or non-live configuration made a request")
			return nil, nil
		})); err == nil {
			t.Fatal("expected config rejection")
		}
	}
}

func TestRunGeneratesFrozenOutputsButNeverCallsUngradedPassing(t *testing.T) {
	suite, _ := LoadSuite()
	suite.Cases[0].Criteria[0].Expected += " RUBRIC_ONLY_NEVER_GENERATE"
	a, err := Run(context.Background(), testConfig(), suite, scriptedProvider(t, suite, false, false))
	if err != nil || a.Status != "ungraded" || a.CallCount != 2 || len(a.Results) != 1 || a.PolicyVersion != ai.SummaryPolicyVersion || a.PromptVersion != ai.SummaryPromptVersion || !a.HumanReviewRequired {
		t.Fatalf("unexpected ungraded run: %v %+v", err, a)
	}
	if a.Results[0].Output.Brief != suite.Controls[0].Output.Brief || a.Results[0].Judgment != nil || a.Results[0].InputHash != suite.Cases[0].InputHash() {
		t.Fatal("missing frozen output/input provenance")
	}
	if a.Results[0].Counts.Source.NonWhitespace == 0 || a.Results[0].Counts.Brief.Han == 0 {
		t.Fatal("missing descriptive counts")
	}
}

func TestRunCalibratesPositiveAndNegativeControlsBeforeAcceptingGrades(t *testing.T) {
	suite, _ := LoadSuite()
	cfg := testConfig()
	cfg.Grade, cfg.MaxCalls = true, len(suite.Controls)+3 // controls + two summary stages + one judge
	a, err := Run(context.Background(), cfg, suite, scriptedProvider(t, suite, false, false))
	if err != nil || a.Status != "graded_pass" || !a.Calibrated || a.CallCount != len(suite.Controls)+3 || len(a.Calibration) != len(suite.Controls) || a.Results[0].Judgment.Verdict() != Pass {
		t.Fatalf("expected calibrated grade: %v %+v", err, a)
	}
	for _, tc := range []struct {
		name             string
		lenient, unknown bool
	}{
		{"overly lenient", true, false},
		{"uncertain", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Run(context.Background(), cfg, suite, scriptedProvider(t, suite, tc.lenient, tc.unknown))
			if err != nil || a.Status != "failed" || a.Calibrated || len(a.Results) != 0 {
				t.Fatalf("invalid judge must not certify/generate run: %v %+v", err, a)
			}
		})
	}
}

func TestRunBudgetIncludesJudgesAndLengthRetries(t *testing.T) {
	suite, _ := LoadSuite()
	cfg := testConfig()
	cfg.Grade, cfg.MaxCalls = true, len(suite.Controls)+2
	a, err := Run(context.Background(), cfg, suite, scriptedProvider(t, suite, false, false))
	if err != nil || a.Status != "incomplete" || a.CallCount != len(suite.Controls)+2 || a.BudgetDenied == 0 {
		t.Fatalf("judge must share hard budget: %v %+v", err, a)
	}
	cfg.Grade, cfg.MaxCalls = false, 1
	a, err = Run(context.Background(), cfg, suite, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return wireResponse(`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`), nil
	}))
	if err != nil || a.Status != "incomplete" || a.CallCount != 1 || a.BudgetDenied == 0 || a.Results[0].Status != "incomplete" {
		t.Fatalf("retry escaped request budget: %v %+v", err, a)
	}
}

func TestRunPersistsModelJudgmentTextForInvalidQuotesAndRedactsCredentials(t *testing.T) {
	suite, _ := LoadSuite()
	for _, phase := range []string{"calibration", "grading"} {
		t.Run(phase, func(t *testing.T) {
			cfg := testConfig()
			cfg.Grade = true
			base := scriptedProvider(t, suite, false, false)
			j := testJudgment(suite.Cases[0], suite.Controls[0].Output, Pass)
			j.Criteria[0].SourceQuote = "invented quote"
			j.Criteria[0].Reason = "invalid citation; provider echoed test-secret"
			raw, _ := json.Marshal(j)
			a, err := Run(context.Background(), cfg, suite, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				scope, _ := r.Context().Value(scopeKey{}).(callScope)
				if scope.kind != phase {
					return base.RoundTrip(r)
				}
				answer, _ := json.Marshal(string(raw))
				return wireResponse(fmt.Sprintf(`{"model":"judge","choices":[{"message":{"content":%s},"finish_reason":"stop"}],"provider_debug":"must not persist"}`, answer)), nil
			}))
			if err != nil || a.Status != "incomplete" {
				t.Fatalf("bad evidence must remain incomplete: %v %+v", err, a)
			}
			var saved, code string
			if phase == "calibration" {
				saved, code = a.Calibration[0].JudgmentText, a.Calibration[0].ErrorCode
			} else {
				saved, code = a.Results[0].JudgmentText, a.Results[0].ErrorCode
			}
			if saved != string(raw) || code != "judgment_source_quote_invalid" {
				t.Fatalf("model text/category lost before parser rejected quote: text=%q code=%q", saved, code)
			}
			path := filepath.Join(t.TempDir(), "raw-judge.json")
			if err := SaveArtifact(path, a, []string{cfg.Generator.Key}); err != nil {
				t.Fatal(err)
			}
			persisted, _ := os.ReadFile(path)
			if strings.Contains(string(persisted), "test-secret") || strings.Contains(string(persisted), "provider_debug") || !strings.Contains(string(persisted), "[REDACTED]") || !strings.Contains(string(persisted), "judgment_text") {
				t.Fatal("safe model message text must persist with secret redaction, without the provider wrapper")
			}
		})
	}
}

func TestRunUncertainOrMalformedLiveGradeCannotPassAfterCalibration(t *testing.T) {
	suite, _ := LoadSuite()
	for _, malformed := range []bool{false, true} {
		cfg := testConfig()
		cfg.Grade = true
		base := scriptedProvider(t, suite, false, false)
		a, err := Run(context.Background(), cfg, suite, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			scope, _ := r.Context().Value(scopeKey{}).(callScope)
			if scope.kind != "grading" {
				return base.RoundTrip(r)
			}
			j := testJudgment(suite.Cases[0], suite.Controls[0].Output, Unknown)
			raw, _ := json.Marshal(j)
			if malformed {
				raw = []byte(`{"overall":"pass"}`)
			}
			answer, _ := json.Marshal(string(raw))
			return wireResponse(fmt.Sprintf(`{"choices":[{"message":{"content":%s},"finish_reason":"stop"}]}`, answer)), nil
		}))
		want := "failed"
		if malformed {
			want = "incomplete"
		}
		if err != nil || !a.Calibrated || a.Status != want {
			t.Fatalf("unknown/malformed grading escaped fail-closed policy: %v %+v", err, a)
		}
		if malformed && (a.Results[0].JudgmentText != `{"overall":"pass"}` || a.Results[0].ErrorCode != "judgment_json_invalid") {
			t.Fatal("malformed model JSON must remain available with a safe parser category")
		}
	}
}

func TestRunSupportsEveryServerGenerationRouteWithoutLiveNetwork(t *testing.T) {
	suite, _ := LoadSuite()
	image := filepath.Join(t.TempDir(), "fixture.jpg")
	if err := os.WriteFile(image, []byte("test-only image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, route := range []Route{Default, Stream, Custom, CustomStream, Vision, VisionStream} {
		t.Run(string(route), func(t *testing.T) {
			cfg := testConfig()
			cfg.Routes, cfg.ImagePaths = []Route{route}, []string{image}
			cfg.BriefTemplate = "CUSTOM_ROUTE_BRIEF {title} {content}"
			cfg.DetailedTemplate = "CUSTOM_ROUTE_DETAIL {title} {content}"
			provider := scriptedProvider(t, suite, false, false)
			a, err := Run(context.Background(), cfg, suite, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, err := r.GetBody()
				if err != nil {
					t.Fatal(err)
				}
				defer body.Close()
				var req struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				wantStream := route == Stream || route == CustomStream || route == VisionStream
				if req.Stream != wantStream {
					t.Error("runner dispatched to wrong streaming API")
				}
				content := req.Messages[len(req.Messages)-1].Content
				if route == Vision || route == VisionStream {
					var blocks []map[string]any
					if json.Unmarshal(content, &blocks) != nil || len(blocks) < 2 || blocks[1]["type"] != "image_url" {
						t.Error("vision route silently became text-only")
					}
				} else if route == Custom || route == CustomStream {
					if !strings.Contains(string(content), "CUSTOM_ROUTE_") {
						t.Error("custom route ignored its configured template")
					}
				}
				return provider.RoundTrip(r)
			}))
			if err != nil || a.Status != "ungraded" || a.CallCount != 2 || a.Results[0].Route != route || a.Results[0].Status != "ungraded" {
				t.Fatalf("route failed: %v %+v", err, a)
			}
		})
	}
}

func TestArtifactIsDurablePrivateAndRedactsSecretsEvenIfProviderEchoesThem(t *testing.T) {
	suite, _ := LoadSuite()
	a := &Artifact{Status: "ungraded", Results: []CaseResult{{Output: Output{Brief: "echo test-secret", Detailed: "test-secret"}}}}
	path := filepath.Join(t.TempDir(), "run.json")
	if err := SaveArtifact(path, a, []string{"test-secret"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), "test-secret") || !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("secret persisted: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0600 {
		t.Fatal("artifact must be private")
	}
	if SaveArtifact(path, a, nil) == nil {
		t.Fatal("must not silently overwrite a durable prior run")
	}
	// Artifacts must be parseable without the model, source sites, or a database.
	var parsed Artifact
	if json.Unmarshal(b, &parsed) != nil || suite.Version == "" {
		t.Fatal("invalid durable JSON")
	}
}

func TestArtifactRedactsOverlappingAndEscapedSecretValuesCompletely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	a := &Artifact{Status: "ungraded", Results: []CaseResult{{Output: Output{Brief: "key-prefix-extended", Detailed: "key\"with\\escapes"}}}}
	if err := SaveArtifact(path, a, []string{"key-prefix", "key-prefix-extended", "key\"with\\escapes"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var restored Artifact
	if json.Unmarshal(raw, &restored) != nil || restored.Results[0].Output.Brief != "[REDACTED]" || restored.Results[0].Output.Detailed != "[REDACTED]" {
		t.Fatalf("incomplete secret redaction: %s", raw)
	}
}

func TestArtifactRedactsEscapedCredentialsInsideJudgmentJSONText(t *testing.T) {
	secret := "test\"secret\\suffix"
	message, _ := json.Marshal(map[string]string{"reason": secret})
	a := &Artifact{Calibration: []CalibrationResult{{JudgmentText: string(message)}}, Results: []CaseResult{{JudgmentText: string(message)}}}
	path := filepath.Join(t.TempDir(), "nested-secret.json")
	if err := SaveArtifact(path, a, []string{secret}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var restored Artifact
	if json.Unmarshal(raw, &restored) != nil {
		t.Fatal("artifact must remain valid JSON")
	}
	for _, text := range []string{restored.Calibration[0].JudgmentText, restored.Results[0].JudgmentText} {
		var decoded map[string]string
		if json.Unmarshal([]byte(text), &decoded) != nil || decoded["reason"] != "[REDACTED]" {
			t.Fatal("configured credentials must be redacted even inside the model’s JSON message text")
		}
	}
}

func TestTextCountsAreDescriptiveAcrossLanguages(t *testing.T) {
	c := CountText("甲 乙\nAUM epoch-2")
	if c.Han != 2 || c.Words != 3 || c.NonWhitespace != 12 {
		t.Fatalf("wrong explicit count semantics: %+v", c)
	}
}
