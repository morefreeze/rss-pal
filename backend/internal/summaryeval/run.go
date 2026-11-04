package summaryeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/bytedance/rss-pal/internal/ai"
)

type Route string

const (
	Default      Route = "default"
	Stream       Route = "stream"
	Custom       Route = "custom"
	CustomStream Route = "custom-stream"
	Vision       Route = "vision"
	VisionStream Route = "vision-stream"
)

const defaultBriefTemplate = "请用中文提炼文章核心要点。\n标题：{title}\n内容：{content}"
const defaultDetailedTemplate = "请用中文概括文章的重要背景、依据和结论。\n标题：{title}\n内容：{content}"

// Provider configuration stays in memory; artifacts never serialize this type.
type Provider struct {
	Key, BaseURL, Model string
}

type Config struct {
	Live, Grade                     bool
	MaxCalls                        int
	Generator, Judge                Provider
	Routes                          []Route
	CaseIDs                         []string
	Split                           string
	BriefTemplate, DetailedTemplate string
	ImagePaths                      []string
	Revision                        string
	Dirty                           bool
	SuiteFileHash                   string
}

type TextCounts struct {
	NonWhitespace int `json:"non_whitespace_runes"`
	Han           int `json:"han_characters"`
	Words         int `json:"ascii_alphanumeric_words"`
}

type Counts struct {
	Source   TextCounts `json:"source"`
	Brief    TextCounts `json:"brief"`
	Detailed TextCounts `json:"detailed"`
}

type CaseResult struct {
	Case         Case      `json:"input"`
	InputHash    string    `json:"input_sha256"`
	Route        Route     `json:"route"`
	Output       Output    `json:"output"`
	Counts       Counts    `json:"counts"`
	Status       string    `json:"status"`
	Judgment     *Judgment `json:"judgment,omitempty"`
	JudgmentText string    `json:"judgment_text,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Calls        []int     `json:"call_numbers"`
}

type CalibrationResult struct {
	ControlID    string    `json:"control_id"`
	CaseID       string    `json:"case_id"`
	Expected     Verdict   `json:"expected"`
	Matched      bool      `json:"matched"`
	Judgment     *Judgment `json:"judgment,omitempty"`
	JudgmentText string    `json:"judgment_text,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
}

type ImageInput struct {
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type Artifact struct {
	SchemaVersion       int                 `json:"schema_version"`
	SuiteVersion        string              `json:"suite_version"`
	SuiteHash           string              `json:"suite_sha256"`
	SuiteFileHash       string              `json:"suite_file_sha256,omitempty"`
	PolicyVersion       string              `json:"policy_version"`
	PromptVersion       string              `json:"prompt_version,omitempty"`
	GraderVersion       string              `json:"grader_version"`
	CodeRevision        string              `json:"code_revision"`
	WorkingTreeDirty    bool                `json:"working_tree_dirty"`
	StartedAt           time.Time           `json:"started_at"`
	FinishedAt          time.Time           `json:"finished_at"`
	RequestedModel      string              `json:"requested_model"`
	RequestedJudgeModel string              `json:"requested_judge_model,omitempty"`
	BriefTemplateHash   string              `json:"brief_template_sha256"`
	DetailTemplateHash  string              `json:"detailed_template_sha256"`
	ImageInputs         []ImageInput        `json:"image_inputs,omitempty"`
	Status              string              `json:"status"`
	HumanReviewRequired bool                `json:"human_review_required"`
	PlannedResults      int                 `json:"planned_results"`
	Calibrated          bool                `json:"calibrated"`
	Calibration         []CalibrationResult `json:"calibration"`
	Results             []CaseResult        `json:"results"`
	MaxCalls            int                 `json:"max_calls"`
	CallCount           int                 `json:"call_count"`
	BudgetDenied        int                 `json:"budget_denied"`
	Calls               []CallRecord        `json:"calls"`
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-]{0,127}$`)

func validModel(s string) bool { return modelPattern.MatchString(s) }

func validateProvider(p Provider) error {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !validModel(p.Model) || strings.TrimSpace(p.Key) == "" {
		return errors.New("provider requires key, bounded model ID, and a base URL without userinfo/query/fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return errors.New("provider URL must use HTTPS or loopback HTTP")
	}
	return nil
}

func prepareConfig(c Config, s Suite) (Config, []Case, []ImageInput, error) {
	if !c.Live {
		return c, nil, nil, errors.New("model calls require explicit --live")
	}
	if c.MaxCalls < 1 || c.MaxCalls > 256 {
		return c, nil, nil, errors.New("max-calls must be explicitly set from 1 to 256")
	}
	if err := validateProvider(c.Generator); err != nil {
		return c, nil, nil, err
	}
	if c.Grade {
		if c.Judge.BaseURL != "" && c.Judge.BaseURL != c.Generator.BaseURL && c.Judge.Key == "" {
			return c, nil, nil, errors.New("a distinct judge endpoint requires its own explicit key")
		}
		if c.Judge.BaseURL == "" {
			c.Judge.BaseURL = c.Generator.BaseURL
		}
		if c.Judge.Key == "" {
			c.Judge.Key = c.Generator.Key
		}
		if c.Judge.Model == "" {
			c.Judge.Model = c.Generator.Model
		}
		if err := validateProvider(c.Judge); err != nil {
			return c, nil, nil, err
		}
	}
	if c.BriefTemplate == "" {
		c.BriefTemplate = defaultBriefTemplate
	}
	if c.DetailedTemplate == "" {
		c.DetailedTemplate = defaultDetailedTemplate
	}
	if len(c.BriefTemplate) > 64<<10 || len(c.DetailedTemplate) > 64<<10 {
		return c, nil, nil, errors.New("custom templates must be at most 64 KiB")
	}
	if c.Revision == "" {
		c.Revision = "unknown"
	}
	if len(c.Routes) == 0 {
		c.Routes = []Route{Default}
	}
	seen := map[Route]bool{}
	needsImage := false
	for _, route := range c.Routes {
		switch route {
		case Default, Stream, Custom, CustomStream:
		case Vision, VisionStream:
			needsImage = true
		default:
			return c, nil, nil, errors.New("unknown generation route")
		}
		if seen[route] {
			return c, nil, nil, errors.New("duplicate generation route")
		}
		seen[route] = true
	}
	var images []ImageInput
	if needsImage {
		if len(c.ImagePaths) == 0 || len(c.ImagePaths) > 4 {
			return c, nil, nil, errors.New("vision routes require 1–4 explicitly supplied source JPEG images")
		}
		for _, path := range c.ImagePaths {
			f, err := os.Open(path)
			if err != nil {
				return c, nil, nil, errors.New("unable to read source image")
			}
			b, err := io.ReadAll(io.LimitReader(f, captureLimit+1))
			f.Close()
			if err != nil || len(b) == 0 || len(b) > captureLimit {
				return c, nil, nil, errors.New("source image is empty or exceeds 8 MiB")
			}
			images = append(images, ImageInput{SHA256: hashText(string(b)), Bytes: len(b)})
		}
	}
	if c.Split != "" && c.Split != "all" && c.Split != "train" && c.Split != "held_out" {
		return c, nil, nil, errors.New("split must be all, train, or held_out")
	}
	ids := map[string]bool{}
	for _, id := range c.CaseIDs {
		if ids[id] {
			return c, nil, nil, errors.New("duplicate case ID")
		}
		ids[id] = true
	}
	var selected []Case
	for _, fixture := range s.Cases {
		if len(c.CaseIDs) > 0 && !ids[fixture.ID] {
			continue
		}
		delete(ids, fixture.ID)
		if c.Split == "" || c.Split == "all" || fixture.Split == c.Split {
			selected = append(selected, fixture)
		}
	}
	if len(ids) != 0 || len(selected) == 0 {
		return c, nil, nil, errors.New("selection contains unknown cases or is empty")
	}
	return c, selected, images, nil
}

// Run performs no database operations. A non-nil base permits wholly offline
// transport tests. Live production calls require both Live and an explicit cap.
func Run(ctx context.Context, cfg Config, suite Suite, base http.RoundTripper) (*Artifact, error) {
	if err := ValidateSuite(suite); err != nil {
		return nil, err
	}
	cfg, cases, images, err := prepareConfig(cfg, suite)
	if err != nil {
		return nil, err
	}
	tr := NewBudgetTransport(base, cfg.MaxCalls)
	client := &http.Client{Transport: tr, Timeout: 3 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	a := &Artifact{SchemaVersion: 1, SuiteVersion: suite.Version, SuiteHash: suite.Hash(), SuiteFileHash: cfg.SuiteFileHash, PolicyVersion: ai.SummaryPolicyVersion,
		PromptVersion: ai.SummaryPromptVersion, GraderVersion: GraderVersion, CodeRevision: cfg.Revision, WorkingTreeDirty: cfg.Dirty, StartedAt: time.Now().UTC(),
		RequestedModel: cfg.Generator.Model, BriefTemplateHash: hashText(cfg.BriefTemplate), DetailTemplateHash: hashText(cfg.DetailedTemplate),
		ImageInputs: images, Status: "incomplete", HumanReviewRequired: true, PlannedResults: len(cases) * len(cfg.Routes), MaxCalls: cfg.MaxCalls}
	defer func() {
		a.FinishedAt, a.Calls = time.Now().UTC(), tr.Records()
		a.CallCount, a.BudgetDenied = tr.CallCount(), tr.DeniedCount()
	}()
	if cfg.Grade {
		a.RequestedJudgeModel = cfg.Judge.Model
		for _, control := range suite.Controls {
			var fixture Case
			for _, c := range suite.Cases {
				if c.ID == control.CaseID {
					fixture = c
				}
			}
			j, text, gradeErr := judge(withScope(ctx, "calibration", control.CaseID, ""), client, tr, cfg.Judge, fixture, control.Output)
			cr := CalibrationResult{ControlID: control.ID, CaseID: control.CaseID, Expected: control.Expected, JudgmentText: text}
			if gradeErr != nil {
				cr.ErrorCode = judgmentErrorCode(gradeErr, "control_grading_incomplete")
			} else {
				cr.Judgment, cr.Matched = &j, ControlAccepted(control, j)
			}
			a.Calibration = append(a.Calibration, cr)
			if gradeErr != nil {
				return a, nil
			}
			if !cr.Matched {
				a.Status = "failed"
				return a, nil
			}
		}
		a.Calibrated = true
	}
	s := ai.NewSummarizerWithModel(cfg.Generator.Key, cfg.Generator.BaseURL, cfg.Generator.Model)
	s.SetHTTPClient(client)
	s.SetAdmission(nil)
	s.DisableArticleRouting() // explicitly requested model, not implicit GLM routing
	s.SetVisionModel(cfg.Generator.Model)
loop:
	for _, fixture := range cases {
		for _, route := range cfg.Routes {
			start := tr.CallCount()
			r := CaseResult{Case: fixture, InputHash: fixture.InputHash(), Route: route, Status: "incomplete"}
			output, genErr := generate(withScope(ctx, "generation", fixture.ID, route), s, fixture, route, cfg)
			r.Output = output
			r.Counts = Counts{Source: CountText(fixture.Content), Brief: CountText(output.Brief), Detailed: CountText(output.Detailed)}
			if genErr != nil || !FinalCompletionsOK(tr.Records()[start:]) {
				r.ErrorCode = "generation_incomplete"
			} else if !cfg.Grade {
				r.Status = "ungraded"
			} else {
				j, text, gradeErr := judge(withScope(ctx, "grading", fixture.ID, route), client, tr, cfg.Judge, fixture, output)
				r.JudgmentText = text
				if gradeErr != nil {
					r.ErrorCode = judgmentErrorCode(gradeErr, "grading_incomplete")
				} else {
					r.Judgment = &j
					r.Status = "failed"
					if j.Verdict() == Pass {
						r.Status = "graded_pass"
					}
				}
			}
			for n := start + 1; n <= tr.CallCount(); n++ {
				r.Calls = append(r.Calls, n)
			}
			a.Results = append(a.Results, r)
			if ctx.Err() != nil || tr.DeniedCount() > 0 {
				break loop
			}
		}
	}
	if len(a.Results) != a.PlannedResults {
		return a, nil
	}
	a.Status = "graded_pass"
	if !cfg.Grade {
		a.Status = "ungraded"
	}
	for _, r := range a.Results {
		if r.Status == "incomplete" {
			a.Status = "incomplete"
			return a, nil
		}
		if r.Status == "failed" {
			a.Status = "failed"
		}
	}
	return a, nil
}

func generate(ctx context.Context, s *ai.Summarizer, c Case, route Route, cfg Config) (Output, error) {
	var result *ai.SummaryResult
	var err error
	ignoreDelta := func(string) {}
	switch route {
	case Default:
		result, err = s.Summarize(ctx, c.Title, c.Content)
	case Stream:
		result, err = s.SummarizeStream(ctx, c.Title, c.Content, ignoreDelta, ignoreDelta)
	case Custom:
		result, err = s.SummarizeWithTemplate(ctx, c.Title, c.Content, cfg.BriefTemplate, cfg.DetailedTemplate)
	case CustomStream:
		result, err = s.SummarizeWithTemplateStream(ctx, c.Title, c.Content, cfg.BriefTemplate, cfg.DetailedTemplate, ignoreDelta, ignoreDelta)
	case Vision:
		result, err = s.SummarizeWithImages(ctx, c.Title, c.Content, cfg.ImagePaths, nil)
	case VisionStream:
		result, err = s.SummarizeWithImagesStream(ctx, c.Title, c.Content, cfg.ImagePaths, nil, ignoreDelta, ignoreDelta)
	}
	if err != nil || result == nil {
		return Output{}, errors.New("summary generation failed; raw error omitted")
	}
	return Output{Brief: result.Brief, Detailed: result.Detailed}, nil
}

func judge(ctx context.Context, client *http.Client, tr *BudgetTransport, p Provider, c Case, output Output) (Judgment, string, error) {
	start := tr.CallCount()
	payload := map[string]any{"model": p.Model, "max_tokens": 4000, "response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{{"role": "system", "content": "You independently grade source faithfulness. Return only the requested JSON schema."}, {"role": "user", "content": JudgePrompt(c, output)}}}
	if strings.HasPrefix(p.Model, "glm-5") {
		payload["reasoning_effort"] = "low"
	}
	u, _ := url.Parse(p.BaseURL)
	if u.Hostname() == "api.openai.com" && (strings.HasPrefix(p.Model, "gpt-5") || strings.HasPrefix(p.Model, "o1") || strings.HasPrefix(p.Model, "o3") || strings.HasPrefix(p.Model, "o4")) {
		payload["max_completion_tokens"] = payload["max_tokens"]
		delete(payload, "max_tokens")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Judgment{}, "", errors.New("unable to create grading request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.Key)
	resp, err := client.Do(req)
	if err != nil {
		return Judgment{}, "", errors.New("grading request failed; raw error omitted")
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, captureLimit+1))
	resp.Body.Close()
	if resp.StatusCode != 200 || readErr != nil || len(raw) > captureLimit || !FinalCompletionsOK(tr.Records()[start:]) {
		return Judgment{}, "", errors.New("grading completion incomplete")
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Choices) != 1 {
		return Judgment{}, "", errors.New("invalid grading response")
	}
	// The provider envelope is bounded by captureLimit. Preserve only the actual
	// message text (including invalid grading JSON), never the envelope/headers.
	text := response.Choices[0].Message.Content
	j, err := ParseJudgment([]byte(text), c, output)
	return j, text, err
}

// CountText is diagnostic only. Cross-language character ratios are never a
// quality gate; Words counts contiguous ASCII alphanumeric sequences.
func CountText(text string) TextCounts {
	var c TextCounts
	inWord := false
	for _, r := range text {
		if !unicode.IsSpace(r) {
			c.NonWhitespace++
		}
		if unicode.Is(unicode.Han, r) {
			c.Han++
		}
		word := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if word && !inWord {
			c.Words++
		}
		inWord = word
	}
	return c
}

// SaveArtifact creates a new private file. Replacing prior evidence is an
// explicit caller decision; accidental reuse of a path is an error.
func SaveArtifact(path string, a *Artifact, secrets []string) error {
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return errors.New("unable to encode evaluation artifact")
	}
	var patterns []string
	for _, secret := range secrets {
		if secret != "" {
			// Values have one JSON layer; retained model JSON text has two.
			// Match both before replacing shorter overlapping credentials.
			encoded := secret
			for depth := 0; depth < 2; depth++ {
				quoted, _ := json.Marshal(encoded)
				encoded = string(quoted[1 : len(quoted)-1])
				patterns = append(patterns, encoded)
			}
		}
	}
	sort.SliceStable(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	for _, pattern := range patterns {
		raw = bytes.ReplaceAll(raw, []byte(pattern), []byte("[REDACTED]"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("artifact path must be writable and must not already exist")
	}
	if _, err = fmt.Fprintln(f, string(raw)); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		return errors.New("unable to persist evaluation artifact")
	}
	return nil
}
