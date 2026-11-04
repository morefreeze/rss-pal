package summaryeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func suiteBytes(t *testing.T, s Suite) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func exactFileHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Use fresh synthetic inputs rather than copying the private corpus into tests.
func fiveClassSuite(t *testing.T) Suite {
	t.Helper()
	s, err := LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"conditional_cardinality", "claim_status"} {
		for i, id := range []string{class, class + "-h1", class + "-h2"} {
			c := Case{ID: id, Split: "held_out", FailureClass: class, Title: id,
				Content:  id + " is a synthetic source, not an expected summary.",
				Source:   Source{Scope: "independent_variant", BasedOn: class, EvidenceRef: "synthetic", Note: "test-only independent wording"},
				Criteria: []Criterion{{ID: "distinction", Expected: "keep source role", Prohibited: []string{"invented fact"}, SourceEvidence: "synthetic source"}}}
			if i == 0 {
				c.Split = "train"
				c.Source = Source{Scope: "frozen_excerpt", URL: "https://example.org/" + class, EvidenceRef: "synthetic", Note: "test-only source"}
			}
			s.Cases = append(s.Cases, c)
		}
		s.Controls = append(s.Controls,
			Control{ID: class + "-positive", CaseID: class, Expected: Pass, Output: Output{Brief: "faithful brief", Detailed: "faithful detail"}},
			Control{ID: class + "-negative", CaseID: class, Expected: Fail, MustFail: []string{"distinction"}, Output: Output{Brief: "wrong brief", Detailed: "wrong detail"}})
	}
	return s
}

func TestSuiteChecksEveryDeclaredClass(t *testing.T) {
	s := fiveClassSuite(t)
	if err := ValidateSuite(s); err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"metric_attribution", "nested_termination", "excluded_domain", "conditional_cardinality", "claim_status"} {
		for _, missing := range []string{"held_out", "positive", "negative"} {
			t.Run(class+"/"+missing, func(t *testing.T) {
				candidate := fiveClassSuite(t)
				if missing == "held_out" {
					for i, c := range candidate.Cases {
						if c.FailureClass == class && c.Split == "held_out" {
							candidate.Cases = append(candidate.Cases[:i], candidate.Cases[i+1:]...)
							break
						}
					}
				} else {
					classes := map[string]string{}
					for _, c := range candidate.Cases {
						classes[c.ID] = c.FailureClass
					}
					var controls []Control
					for _, ctrl := range candidate.Controls {
						if classes[ctrl.CaseID] == class && (missing == "positive" && ctrl.Expected == Pass || missing == "negative" && ctrl.Expected == Fail) {
							continue
						}
						controls = append(controls, ctrl)
					}
					candidate.Controls = controls
				}
				if ValidateSuite(candidate) == nil {
					t.Fatal("every declared class requires two held-outs and both control polarities")
				}
			})
		}
	}
}

func TestSuiteRejectsNonIndependentVariantsAndBlankClass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Suite)
	}{
		{"blank class", func(s *Suite) { s.Cases[0].FailureClass = " \t" }},
		{"unknown parent", func(s *Suite) { s.Cases[1].Source.BasedOn = "missing" }},
		{"held-out parent", func(s *Suite) { s.Cases[1].Source.BasedOn = s.Cases[2].ID }},
		{"different class parent", func(s *Suite) { s.Cases[1].Source.BasedOn = "018" }},
		{"copied parent", func(s *Suite) { s.Cases[1].Content = s.Cases[0].Content; s.Cases[1].Criteria = s.Cases[0].Criteria }},
		{"same body different title", func(s *Suite) { s.Cases[2].Content = s.Cases[1].Content; s.Cases[2].Criteria = s.Cases[1].Criteria }},
		{"whitespace only variant", func(s *Suite) {
			s.Cases[2].Content = "\n" + s.Cases[1].Content + "\t"
			s.Cases[2].Criteria = s.Cases[1].Criteria
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := LoadSuite()
			tc.mutate(&s)
			if ValidateSuite(s) == nil {
				t.Fatal("invalid source independence accepted")
			}
		})
	}
}

func TestSuiteJSONRejectsLossyUnicodeAndKeyAliases(t *testing.T) {
	s, _ := LoadSuite()
	raw := string(suiteBytes(t, s))
	for _, bad := range []string{
		strings.Replace(raw, `"title":`, `"title":"`+string([]byte{0xff})+`","title":`, 1),
		strings.Replace(raw, `"title":`, `"title":"\ud800","title":`, 1),
		strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(raw, `"schema_version"`, `"SCHEMA_VERSION"`, 1),
		strings.Replace(raw, `"schema_version"`, `"ſchema_version"`, 1),
		strings.Replace(raw, `"version":`, `"version":"\udfff","unknown":`, 1),
	} {
		if _, err := DecodeSuite([]byte(bad)); err == nil {
			t.Fatal("lossy Unicode or aliased JSON key accepted")
		}
	}
	// Isolate Unicode from duplicate/unknown keys: replace a complete value.
	for _, value := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, `"` + string([]byte{0xff}) + `"`} {
		s.Cases[0].Title = "UNICODE_SENTINEL"
		bad := strings.Replace(string(suiteBytes(t, s)), `"UNICODE_SENTINEL"`, value, 1)
		if _, err := DecodeSuite([]byte(bad)); err == nil {
			t.Fatalf("lossy Unicode value accepted: %q", value)
		}
	}
	s.Cases[0].Title = "UNICODE_SENTINEL"
	for _, value := range []string{`"\ud83d\ude80"`, `"literal \\ud800 and replacement �"`} {
		valid := strings.Replace(string(suiteBytes(t, s)), `"UNICODE_SENTINEL"`, value, 1)
		if _, err := DecodeSuite([]byte(valid)); err != nil {
			t.Fatalf("valid Unicode/escaped literal rejected: %v", err)
		}
	}
}

func TestLoadSuiteFileBindsExactBytesAndRejectsUnsafeFiles(t *testing.T) {
	// macOS temporary roots can themselves be symlinks (/var); resolve the
	// test root deliberately so that only the hostile test paths contain them.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := LoadSuite()
	raw := append(suiteBytes(t, s), '\n')
	path := filepath.Join(dir, "suite.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	wantHash := exactFileHash(raw)
	loaded, gotHash, err := LoadSuiteFile(path, wantHash)
	if err != nil || loaded.Hash() != s.Hash() || gotHash != wantHash {
		t.Fatalf("exact source binding failed: %v hash=%s", err, gotHash)
	}
	if _, gotHash, err := LoadSuiteFile(path, ""); err != nil || gotHash != wantHash {
		t.Fatalf("offline unguarded hash preparation failed: %v", err)
	}
	symlink := filepath.Join(dir, "alias.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	parentAlias := filepath.Join(dir, "alias-dir")
	if err := os.Symlink(dir, parentAlias); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "oversize.json")
	if err := os.WriteFile(large, []byte(strings.Repeat(" ", (2<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, hash string }{
		{path, strings.Repeat("0", 64)}, {path, "short"}, {"", ""},
		{symlink, ""}, {filepath.Join(parentAlias, "suite.json"), ""},
		{dir + "/./suite.json", ""}, {dir + "/../" + filepath.Base(dir) + "/suite.json", ""},
		{dir, ""}, {fifo, ""}, {large, ""}, {filepath.Join(dir, "missing"), ""},
	} {
		if _, _, err := LoadSuiteFile(tc.path, tc.hash); err == nil {
			t.Fatalf("unsafe file/hash accepted: %s", tc.path)
		}
	}
}
