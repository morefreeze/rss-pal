package model

import "time"

// SummaryGeneration records provenance, not a semantic quality certification.
// Legacy/copied results without traceable inputs intentionally have no value.
type SummaryGeneration struct {
	SchemaVersion          int                      `json:"schema_version"`
	PolicyVersion          string                   `json:"policy_version"`
	PolicyHash             string                   `json:"policy_hash"`
	PromptVersion          string                   `json:"prompt_version"`
	ContentHash            string                   `json:"content_hash"`
	InputHash              string                   `json:"input_hash"`
	TemplateKind           string                   `json:"template_kind"`
	TemplateHash           string                   `json:"template_hash"`
	Execution              string                   `json:"execution"`
	EntryPoint             string                   `json:"entry_point,omitempty"`
	BindingHash            string                   `json:"binding_hash,omitempty"`
	GeneratedAt            time.Time                `json:"generated_at"`
	Stages                 []SummaryGenerationStage `json:"stages"`
	TemplateID             int                      `json:"template_id,omitempty"`
	SourceTemplateBrief    string                   `json:"-"`
	SourceTemplateDetailed string                   `json:"-"`
	// SourceTitle is used for the atomic title CAS, never stored as provenance.
	SourceTitle string `json:"-"`
}

type SummaryGenerationStage struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	ModelSource string `json:"model_source"`
	PromptHash  string `json:"prompt_hash"`
	RequestHash string `json:"request_hash,omitempty"`
}

func (g *SummaryGeneration) BindTemplate(id int, brief, detailed string) {
	if g == nil || id <= 0 {
		return
	}
	g.TemplateID = id
	g.TemplateKind = "custom"
	g.SourceTemplateBrief = brief
	g.SourceTemplateDetailed = detailed
}
