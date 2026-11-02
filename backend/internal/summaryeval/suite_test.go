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
	if len(suite.Cases) != 9 || len(suite.Controls) != 6 {
		t.Fatalf("want 9 cases and 6 controls, got %d/%d", len(suite.Cases), len(suite.Controls))
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
