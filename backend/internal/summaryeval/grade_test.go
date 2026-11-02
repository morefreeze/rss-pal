package summaryeval

import (
	"encoding/json"
	"strings"
	"testing"
)

func testJudgment(c Case, output Output, verdict Verdict) Judgment {
	j := Judgment{SchemaVersion: 1}
	for _, cr := range c.Criteria {
		j.Criteria = append(j.Criteria, CriterionVerdict{ID: cr.ID, Verdict: verdict,
			SourceQuote: cr.SourceEvidence, SummaryQuote: output.Brief, Reason: "Compared the entities, scope, and conditions."})
	}
	return j
}

func TestJudgmentAcceptsEvidenceBackedParaphrasesWithoutExactAnswerMatching(t *testing.T) {
	s, _ := LoadSuite()
	c := s.Cases[0]
	o := s.Controls[0].Output
	j := testJudgment(c, o, Pass)
	raw, _ := json.Marshal(j)
	got, err := ParseJudgment(raw, c, o)
	if err != nil || got.Verdict() != Pass {
		t.Fatalf("faithful paraphrase should be accepted: %v %+v", err, got)
	}
	if strings.Contains(JudgePrompt(c, o), "expected_control_verdict") {
		t.Fatal("grader must not see the calibration answer")
	}
}

func TestGraderVersionAndInstructionsSeparateAssignedLabelsFromVerifiedTruth(t *testing.T) {
	s, _ := LoadSuite()
	if GraderVersion != "semantic-evidence-v2" {
		t.Fatal("changed semantic grading needs a new grader version")
	}
	prompt := JudgePrompt(s.Cases[6], s.Controls[4].Output)
	for _, required := range []string{"verified ground truth", "assigned/reference", "an accurate detail cannot cancel a contradictory brief", "Do not reward rubric keywords alone"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing semantic distinction: %s", required)
		}
	}
}

func TestJudgmentNeverPassesUnknownMissingOrUngroundedCriteria(t *testing.T) {
	s, _ := LoadSuite()
	c, o := s.Cases[0], s.Controls[0].Output
	for _, tc := range []struct {
		name string
		fn   func(*Judgment)
		bad  bool
	}{
		{"unknown", func(j *Judgment) { j.Criteria[0].Verdict = Unknown }, false},
		{"failure", func(j *Judgment) { j.Criteria[0].Verdict = Fail }, false},
		{"missing criterion", func(j *Judgment) { j.Criteria = j.Criteria[1:] }, true},
		{"duplicate criterion", func(j *Judgment) { j.Criteria[1].ID = j.Criteria[0].ID }, true},
		{"empty evidence", func(j *Judgment) { j.Criteria[0].SummaryQuote = "" }, true},
		{"fabricated source quote", func(j *Judgment) { j.Criteria[0].SourceQuote = "invented source quotation" }, true},
		{"whitespace-only source quote", func(j *Judgment) { j.Criteria[0].SourceQuote = "\n" }, true},
		{"fabricated output quote", func(j *Judgment) { j.Criteria[0].SummaryQuote = "not in the candidate" }, true},
		{"missing rationale", func(j *Judgment) { j.Criteria[0].Reason = "" }, true},
		{"invalid verdict", func(j *Judgment) { j.Criteria[0].Verdict = "mostly_pass" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := testJudgment(c, o, Pass)
			tc.fn(&j)
			raw, _ := json.Marshal(j)
			got, err := ParseJudgment(raw, c, o)
			if tc.bad && err == nil {
				t.Fatal("expected parser rejection")
			}
			if !tc.bad && (err != nil || got.Verdict() == Pass) {
				t.Fatalf("unknown/fail must fail closed: %v %+v", err, got)
			}
		})
	}
	for _, raw := range []string{"```json\n{}\n```", "{} {}", `{"schema_version":1,"overall":"pass","criteria":[]}`} {
		if _, err := ParseJudgment([]byte(raw), c, o); err == nil {
			t.Fatal("malformed/extra grading output must be rejected")
		}
	}
	valid, _ := json.Marshal(testJudgment(c, o, Pass))
	duplicate := strings.Replace(string(valid), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)
	if _, err := ParseJudgment([]byte(duplicate), c, o); err == nil {
		t.Fatal("duplicate JSON keys must not silently overwrite grading evidence")
	}
}

func TestJudgmentRejectsNestedCaseAliasOverridingFailWithPass(t *testing.T) {
	s, _ := LoadSuite()
	c, o := s.Cases[0], s.Controls[0].Output
	j := testJudgment(c, o, Pass)
	j.Criteria[0].Verdict = Fail
	raw, _ := json.Marshal(j)
	aliased := strings.Replace(string(raw), `"verdict":"fail"`, `"verdict":"fail","Verdict":"pass"`, 1)
	got, err := ParseJudgment([]byte(aliased), c, o)
	if err == nil {
		t.Fatalf("case alias must not overwrite nested grading evidence: verdict=%s", got.Verdict())
	}
}

func TestJudgmentRejectsUnicodeSchemaAliasOverridingFabricatedEvidence(t *testing.T) {
	s, _ := LoadSuite()
	c, o := s.Cases[0], s.Controls[0].Output
	j := testJudgment(c, o, Pass)
	j.Criteria[0].SourceQuote = "invented evidence"
	raw, _ := json.Marshal(j)
	validQuote, _ := json.Marshal(c.Criteria[0].SourceEvidence)
	aliased := strings.Replace(string(raw), `"source_quote":"invented evidence"`, `"source_quote":"invented evidence","ſource_quote":`+string(validQuote), 1)
	got, err := ParseJudgment([]byte(aliased), c, o)
	if err == nil {
		t.Fatalf("non-ASCII schema alias must not overwrite fabricated evidence: verdict=%s", got.Verdict())
	}
}

func TestJudgmentAllowsShortNonWhitespaceMathematicalEvidence(t *testing.T) {
	s, _ := LoadSuite()
	var c Case
	for _, candidate := range s.Cases {
		if candidate.ID == "019" {
			c = candidate
		}
	}
	o := s.Controls[4].Output
	j := testJudgment(c, o, Pass)
	j.Criteria[1].SourceQuote, j.Criteria[1].SummaryQuote = "y", "y"
	raw, _ := json.Marshal(j)
	if _, err := ParseJudgment(raw, c, o); err != nil {
		t.Fatalf("evidence needs non-whitespace content, not an arbitrary minimum length: %v", err)
	}
}

func TestCalibrationDetectsLenientAndHarshJudges(t *testing.T) {
	s, _ := LoadSuite()
	positive, negative := s.Controls[0], s.Controls[1]
	c := s.Cases[0]
	if !ControlAccepted(positive, testJudgment(c, positive.Output, Pass)) {
		t.Fatal("faithful paraphrase control should pass")
	}
	if ControlAccepted(negative, testJudgment(c, negative.Output, Pass)) {
		t.Fatal("judge that passes the known error must not calibrate")
	}
	if ControlAccepted(positive, testJudgment(c, positive.Output, Fail)) {
		t.Fatal("judge that rejects faithful paraphrases must not calibrate")
	}
	if !ControlAccepted(negative, testJudgment(c, negative.Output, Fail)) {
		t.Fatal("known wrong control should fail its required criterion")
	}
	if ControlAccepted(negative, testJudgment(c, negative.Output, Unknown)) {
		t.Fatal("uncertainty is insufficient calibration")
	}
	partial := testJudgment(c, negative.Output, Fail)
	partial.Criteria[len(partial.Criteria)-1].Verdict = Unknown
	if ControlAccepted(negative, partial) {
		t.Fatal("a negative control with unresolved criteria must not calibrate")
	}
}
