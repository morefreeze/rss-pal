package summaryeval

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFrozenSuiteHasIndependentHeldOutsAndCalibrationControls(t *testing.T) {
	suite, err := LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSuite(suite); err != nil {
		t.Fatal(err)
	}
	if suite.Version != "summary-fidelity-2026-11-02-v3" {
		t.Fatalf("fixture changes require the current input version: %s", suite.Version)
	}
	if len(suite.Cases) != 9 || len(suite.Controls) != 7 {
		t.Fatalf("want 9 cases and 7 controls, got %d/%d", len(suite.Cases), len(suite.Controls))
	}
	heldOut := map[string]int{}
	for _, c := range suite.Cases {
		if c.Split == "held_out" {
			heldOut[c.FailureClass]++
		}
		if c.Source.Scope == "full_article" {
			t.Fatal("short fixtures must not claim full-article coverage")
		}
	}
	for _, class := range []string{"metric_attribution", "nested_termination", "excluded_domain"} {
		if heldOut[class] < 2 {
			t.Fatalf("%s needs two independently worded held-outs", class)
		}
	}
	book := suite.Cases[0]
	if !strings.Contains(book.Content, "2024年还足以维持我的生活，2026年却变为零") || strings.Contains(book.Content, "六位数") {
		t.Fatal("017 must preserve the actual frozen qualifier, not invent a number")
	}
	for _, control := range suite.Controls {
		if strings.TrimSpace(control.Output.Brief) == "" || strings.TrimSpace(control.Output.Detailed) == "" {
			t.Fatalf("control %s is missing a complete candidate", control.ID)
		}
	}
}

func TestAssignedClassRubricHasDistinctGroundTruthNegativeControl(t *testing.T) {
	s, err := LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	var foundAssigned, foundRunnerUp bool
	for _, control := range s.Controls {
		if control.ID == "019-global-runner-up" {
			foundRunnerUp = true
		}
		if control.ID == "019-assigned-as-correct" {
			foundAssigned = true
			if control.Expected != Fail || len(control.MustFail) != 1 || control.MustFail[0] != "assigned_class" || !strings.Contains(control.Output.Brief, "正确类") || !strings.Contains(control.Output.Detailed, "真实类别") {
				t.Fatal("assigned-as-correct control must independently fail the label/ground-truth distinction")
			}
		}
	}
	if !foundAssigned || !foundRunnerUp {
		t.Fatal("both assigned-as-correct and global-runner-up negative controls are required")
	}
	for _, c := range s.Cases {
		if c.ID == "019" {
			for _, cr := range c.Criteria {
				if cr.ID == "assigned_class" && !strings.Contains(cr.Expected, "verified ground truth") {
					t.Fatal("rubric must distinguish annotations from verified ground truth")
				}
			}
		}
	}
}

func TestWorkflowPositiveControlIncludesUnattendedExecution(t *testing.T) {
	s, err := LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range s.Controls {
		if control.ID == "018-faithful-paraphrase" {
			if !strings.Contains(control.Output.text(), "全程无需人类实时参与") {
				t.Fatal("positive control must retain the explicit unattended-execution condition required by the unchanged trigger rubric")
			}
			return
		}
	}
	t.Fatal("missing workflow positive calibration control")
}

func TestSuiteRejectsAmbiguousOrUnsupportedFixtures(t *testing.T) {
	for _, mutate := range []struct {
		name string
		fn   func(*Suite)
	}{
		{"duplicate case", func(s *Suite) { s.Cases[1].ID = s.Cases[0].ID }},
		{"false full article", func(s *Suite) { s.Cases[0].Source.Scope = "full_article" }},
		{"missing evidence", func(s *Suite) { s.Cases[0].Criteria[0].SourceEvidence = "not in the source" }},
		{"missing rubric", func(s *Suite) { s.Cases[0].Criteria = nil }},
		{"unknown control case", func(s *Suite) { s.Controls[0].CaseID = "missing" }},
		{"negative without violated criterion", func(s *Suite) { s.Controls[1].MustFail = nil }},
		{"unknown control criterion", func(s *Suite) { s.Controls[1].MustFail = []string{"made_up"} }},
		{"missing held out", func(s *Suite) { s.Cases[1].Split = "train" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			s, err := LoadSuite()
			if err != nil {
				t.Fatal(err)
			}
			mutate.fn(&s)
			if ValidateSuite(s) == nil {
				t.Fatal("expected invalid fixture schema")
			}
		})
	}
}

func TestSuiteJSONIsStrictAndMatchesEmbeddedInput(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixtures.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := DecodeSuite(raw)
	if err != nil {
		t.Fatal(err)
	}
	embedded, _ := LoadSuite()
	a, _ := json.Marshal(s)
	b, _ := json.Marshal(embedded)
	if string(a) != string(b) {
		t.Fatal("embedded suite changed")
	}
	for _, bad := range []string{string(raw) + " {}", strings.Replace(string(raw), "\"schema_version\"", "\"unknown_field\"", 1)} {
		if _, err := DecodeSuite([]byte(bad)); err == nil {
			t.Fatal("expected strict JSON/schema error")
		}
	}
}
