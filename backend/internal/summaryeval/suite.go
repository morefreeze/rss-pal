// Package summaryeval runs opt-in, bounded summary experiments without a DB.
// Rubrics belong exclusively to the grader and are never generator inputs.
package summaryeval

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

//go:embed testdata/fixtures.v1.json
var fixtureJSON []byte

type Suite struct {
	SchemaVersion int       `json:"schema_version"`
	Version       string    `json:"version"`
	Cases         []Case    `json:"cases"`
	Controls      []Control `json:"controls"`
}

type Case struct {
	ID           string      `json:"id"`
	Split        string      `json:"split"`
	FailureClass string      `json:"failure_class"`
	Title        string      `json:"title"`
	Content      string      `json:"content"`
	Source       Source      `json:"source"`
	Criteria     []Criterion `json:"criteria"`
}

type Source struct {
	URL         string `json:"url,omitempty"`
	EvidenceRef string `json:"evidence_ref"`
	Scope       string `json:"scope"` // frozen_excerpt or independent_variant
	BasedOn     string `json:"based_on,omitempty"`
	Note        string `json:"note"`
}

type Criterion struct {
	ID             string   `json:"id"`
	Expected       string   `json:"expected"`
	Prohibited     []string `json:"prohibited"`
	SourceEvidence string   `json:"source_evidence"`
}

type Output struct {
	Brief    string `json:"brief"`
	Detailed string `json:"detailed"`
}

func (o Output) text() string { return o.Brief + "\n" + o.Detailed }

type Control struct {
	ID       string   `json:"id"`
	CaseID   string   `json:"case_id"`
	Expected Verdict  `json:"expected"`
	MustFail []string `json:"must_fail,omitempty"`
	Output   Output   `json:"output"`
}

func strictJSON(raw []byte, dst any) error {
	if len(raw) > suiteJSONLimit {
		return errors.New("JSON exceeds evaluation limit")
	}
	if !utf8.Valid(raw) || !validJSONUnicode(raw) {
		return errors.New("evaluation JSON must contain lossless Unicode")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := checkJSONKeys(json.NewDecoder(bytes.NewReader(raw)), 0); err != nil {
		return errors.New("invalid or duplicate evaluation JSON keys")
	}
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return errors.New("invalid evaluation JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("evaluation JSON must contain one object")
	}
	return nil
}

func checkJSONKeys(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds evaluation limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("unexpected JSON delimiter")
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			// Schema keys are lowercase ASCII. Unicode case-fold aliases such as
			// long s also match ASCII struct tags in encoding/json.
			ascii := strings.IndexFunc(name, func(r rune) bool { return r > 127 }) < 0
			if !ok || !ascii || name != strings.ToLower(name) || seen[name] {
				return errors.New("invalid, case-aliased, or repeated object key")
			}
			seen[name] = true
		}
		if err := checkJSONKeys(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func LoadSuite() (Suite, error) { return DecodeSuite(fixtureJSON) }

func DecodeSuite(raw []byte) (Suite, error) {
	var s Suite
	if err := strictJSON(raw, &s); err != nil {
		return s, err
	}
	return s, ValidateSuite(s)
}

func ValidateSuite(s Suite) error {
	if s.SchemaVersion != 1 || s.Version == "" || len(s.Cases) == 0 {
		return errors.New("unsupported or empty suite")
	}
	cases := map[string]Case{}
	classes := map[string]bool{}
	held := map[string]map[string]bool{}
	trainInputs := map[string]bool{}
	for _, c := range s.Cases {
		if c.ID == "" || cases[c.ID].ID != "" || c.Title == "" || c.Content == "" || len(c.Content) > 256<<10 || len(c.Criteria) == 0 {
			return errors.New("case has duplicate ID or incomplete input/rubric")
		}
		if c.Split != "train" && c.Split != "held_out" {
			return errors.New("case must declare train or held_out")
		}
		if strings.TrimSpace(c.FailureClass) == "" {
			return errors.New("case must declare a nonblank failure class")
		}
		classes[c.FailureClass] = true
		if c.Source.EvidenceRef == "" || c.Source.Note == "" || (c.Source.Scope != "frozen_excerpt" && c.Source.Scope != "independent_variant") {
			return errors.New("case must identify excerpt/variant provenance")
		}
		if c.Source.Scope == "frozen_excerpt" && c.Source.URL == "" || c.Source.Scope == "independent_variant" && c.Source.BasedOn == "" {
			return errors.New("missing source URL or variant parent")
		}
		ids := map[string]bool{}
		for _, cr := range c.Criteria {
			if cr.ID == "" || ids[cr.ID] || cr.Expected == "" || len(cr.Prohibited) == 0 || cr.SourceEvidence == "" || !strings.Contains(c.Content, cr.SourceEvidence) {
				return fmt.Errorf("case %s has missing, duplicate, or ungrounded criterion", c.ID)
			}
			ids[cr.ID] = true
		}
		cases[c.ID] = c
		if c.Split == "train" {
			trainInputs[independentInput(c.Content)] = true
		}
	}
	for _, c := range s.Cases {
		input := independentInput(c.Content)
		if c.Source.Scope == "independent_variant" {
			parent, ok := cases[c.Source.BasedOn]
			if !ok || parent.Split != "train" || parent.FailureClass != c.FailureClass || independentInput(parent.Content) == input {
				return errors.New("variant requires a same-class training parent and distinct source content")
			}
		}
		if c.Split == "held_out" {
			if held[c.FailureClass] == nil {
				held[c.FailureClass] = map[string]bool{}
			}
			if trainInputs[input] || held[c.FailureClass][input] {
				return errors.New("held-out source content must be distinct from training and other held-outs")
			}
			held[c.FailureClass][input] = true
		}
	}
	controls := map[string]bool{}
	calibrated := map[string]map[Verdict]bool{}
	for _, ctrl := range s.Controls {
		c, ok := cases[ctrl.CaseID]
		if ctrl.ID == "" || controls[ctrl.ID] || !ok || c.Split != "train" || strings.TrimSpace(ctrl.Output.Brief) == "" || strings.TrimSpace(ctrl.Output.Detailed) == "" {
			return errors.New("invalid calibration control")
		}
		if ctrl.Expected != Pass && ctrl.Expected != Fail || ctrl.Expected == Fail && len(ctrl.MustFail) == 0 || ctrl.Expected == Pass && len(ctrl.MustFail) != 0 {
			return errors.New("control must declare a decisive expectation")
		}
		for _, id := range ctrl.MustFail {
			found := false
			for _, cr := range c.Criteria {
				found = found || cr.ID == id
			}
			if !found {
				return errors.New("negative control names unknown criterion")
			}
		}
		if calibrated[c.FailureClass] == nil {
			calibrated[c.FailureClass] = map[Verdict]bool{}
		}
		calibrated[c.FailureClass][ctrl.Expected] = true
		controls[ctrl.ID] = true
	}
	for class := range classes {
		if len(held[class]) < 2 || !calibrated[class][Pass] || !calibrated[class][Fail] {
			return fmt.Errorf("%s requires two held-outs and positive/negative controls", class)
		}
	}
	return nil
}

// Independence checks ignore spacing only; source bytes and evaluation hashes
// are never normalized or rewritten. A title edit cannot create a new input.
func independentInput(content string) string { return strings.Join(strings.Fields(content), " ") }

func hashText(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (s Suite) Hash() string {
	b, _ := json.Marshal(s)
	return hashText(string(b))
}

func (c Case) InputHash() string { return hashText(c.Title + "\x00" + c.Content) }
