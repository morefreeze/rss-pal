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
