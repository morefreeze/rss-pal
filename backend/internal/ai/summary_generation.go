package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

const SummaryPolicyVersion = "faithfulness-2026-11-02-v2"
const SummaryPromptVersion = "summary-prompts-2026-11-02-v1"

func summaryHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SetHTTPClient allows bounded offline evaluation transports without mutating
// the global default transport. Set before concurrent use, as with model config.
func (s *Summarizer) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.httpClient = client
	}
}

func newSummaryGeneration(title, content, briefTemplate, detailedTemplate string) *model.SummaryGeneration {
	kind := "default"
	if briefTemplate != "" || detailedTemplate != "" {
		kind = "custom"
	}
	return &model.SummaryGeneration{SchemaVersion: 1, PolicyVersion: SummaryPolicyVersion,
		PolicyHash: summaryHash(systemGuardrail), PromptVersion: SummaryPromptVersion,
		ContentHash: summaryHash(content), InputHash: summaryHash(title + "\x00" + content),
		TemplateKind: kind, TemplateHash: summaryHash(briefTemplate + "\x00" + detailedTemplate),
		Execution: "server", SourceTitle: title}
}

func (s *Summarizer) beginSummary(title, content, briefTemplate, detailedTemplate string) *Summarizer {
	selected := *s
	selected.generation = newSummaryGeneration(title, content, briefTemplate, detailedTemplate)
	return &selected
}

// Record only requests that produced the accepted output. This handles mixed
// vision/text fallbacks and excludes failed/retried attempts from output lineage.
func (s *Summarizer) captureSummaryWireRequest(req *http.Request) {
	if s.generation == nil || req.GetBody == nil {
		return
	}
	reader, err := req.GetBody()
	if err != nil {
		return
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return
	}
	var input struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
		System   json.RawMessage `json:"system"`
	}
	if json.Unmarshal(body, &input) != nil {
		return
	}
	prompt := input.Messages
	if len(input.System) > 0 {
		prompt, _ = json.Marshal(map[string]json.RawMessage{"system": input.System, "messages": input.Messages})
	}
	s.lastSummaryRequest = &model.SummaryGenerationStage{Model: input.Model, ModelSource: "server-requested", PromptHash: summaryHash(string(prompt)), RequestHash: summaryHash(string(body))}
}

func (s *Summarizer) recordSummaryRequest(_ []byte) {
	if s.generation == nil || s.lastSummaryRequest == nil {
		return
	}
	stage := *s.lastSummaryRequest
	stage.Name = "brief"
	if len(s.generation.Stages) > 0 {
		stage.Name = "detailed"
	}
	s.generation.Stages = append(s.generation.Stages, stage)
	s.lastSummaryRequest = nil
}

func (s *Summarizer) summaryResult(brief, detailed string) *SummaryResult {
	s.generation.GeneratedAt = time.Now().UTC()
	return &SummaryResult{Brief: brief, Detailed: detailed, Generation: s.generation}
}

// Browser execution cannot be attested by the server. Bind the prepared inputs
// to their policy and template; the result remains explicitly client-reported.
func LocalSummaryGeneration(title, content, briefTemplate, detailedTemplate string, templateIDs ...int) *model.SummaryGeneration {
	g := newSummaryGeneration(title, content, briefTemplate, detailedTemplate)
	g.Execution = "client-reported"
	if len(templateIDs) > 0 {
		g.BindTemplate(templateIDs[0], briefTemplate, detailedTemplate)
	}
	brief, detailed := LocalSummaryPrompts(title, content, briefTemplate, detailedTemplate)
	g.BindingHash = summaryHash(strconv.Itoa(g.TemplateID) + "\x00" + g.PolicyVersion + "\x00" + g.InputHash + "\x00" + g.PolicyHash + "\x00" + g.PromptVersion + "\x00" + g.TemplateHash + "\x00" + brief + "\x00" + detailed)
	return g
}

// CompleteLocalSummaryGeneration records hashes of the exact user messages used
// by the browser, including its brief-to-detailed context. No provider URL/key.
func CompleteLocalSummaryGeneration(g *model.SummaryGeneration, modelName, briefPrompt, detailedPrompt, brief string, requestHashes []string) {
	g.GeneratedAt = time.Now().UTC()
	g.EntryPoint = "browser-local"
	g.Stages = []model.SummaryGenerationStage{
		{Name: "brief", Model: modelName, ModelSource: "client-reported", PromptHash: summaryHash(briefPrompt), RequestHash: requestHashes[0]},
		{Name: "detailed", Model: modelName, ModelSource: "client-reported", PromptHash: summaryHash(LocalDetailedSummaryPrompt(detailedPrompt, brief)), RequestHash: requestHashes[1]},
	}
}

func LocalDetailedSummaryPrompt(prompt, brief string) string {
	return prompt + "\n\n前置要点（仅供去重，不作为新的事实来源）：\n" + brief
}
