package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/summaryeval"
)

func externalSuite(t *testing.T) (string, string, summaryeval.Suite) {
	t.Helper()
	suite, err := summaryeval.LoadSuite()
	if err != nil {
		t.Fatal(err)
	}
	suite.Version = "external-cli-source-binding-test"
	suite.Cases[0].Title = "EXTERNAL_SOURCE_TITLE"
	raw, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "external.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return path, hex.EncodeToString(hash[:]), suite
}

func TestCLIExternalSuiteValidationPrintsBothHashesWithoutCredentials(t *testing.T) {
	path, hash, suite := externalSuite(t)
	var out, stderr bytes.Buffer
	code := execute(context.Background(), []string{"--validate", "--suite-file", path}, func(k string) string {
		if strings.Contains(k, "KEY") {
			t.Fatalf("offline validation read credential: %s", k)
		}
		return ""
	}, &out, &stderr, testTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline validation made a request")
		return nil, nil
	}))
	if code != 0 || !strings.Contains(out.String(), suite.Version) || !strings.Contains(out.String(), hash) || !strings.Contains(out.String(), suite.Hash()) || !strings.Contains(out.String(), "semantic evaluation not run") {
		t.Fatalf("missing offline source binding: %d %s %s", code, out.String(), stderr.String())
	}
}

func TestCLIExternalSuitePreflightRejectsBeforeCredentialsRequestsAndArtifact(t *testing.T) {
	path, hash, _ := externalSuite(t)
	for _, flags := range [][]string{
		{"--suite-file", path}, // live external sources always require a hash
		{"--suite-file", path, "--suite-sha256", strings.Repeat("0", 64)},
		{"--suite-file", path, "--suite-sha256", ""},
		{"--suite-sha256", hash},
		{"--suite-file", "", "--suite-sha256", hash},
		{"--suite-file", path + "-missing", "--suite-sha256", hash},
	} {
		outPath := filepath.Join(t.TempDir(), "must-not-exist.json")
		args := append([]string{"--live", "--max-calls", "2", "--out", outPath}, flags...)
		var out, stderr bytes.Buffer
		code := execute(context.Background(), args, func(k string) string {
			if strings.Contains(k, "KEY") {
				t.Fatalf("invalid suite read credential: %s", k)
			}
			return ""
		}, &out, &stderr, testTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid suite made a request")
			return nil, nil
		}))
		if code != 1 {
			t.Fatalf("invalid flags accepted: %v code=%d %s", flags, code, stderr.String())
		}
		if _, err := os.Lstat(outPath); !os.IsNotExist(err) {
			t.Fatal("invalid suite created an artifact")
		}
	}
}

func TestCLIExternalSuiteGeneratesSelectedSourceAndRecordsBinding(t *testing.T) {
	path, hash, suite := externalSuite(t)
	outPath := filepath.Join(t.TempDir(), "external-run.json")
	var out, stderr bytes.Buffer
	calls := 0
	code := execute(context.Background(), []string{"--live", "--max-calls", "2", "--suite-file", path, "--suite-sha256", hash, "--cases", suite.Cases[0].ID, "--out", outPath},
		func(k string) string {
			return map[string]string{"AI_API_KEY": "test-secret", "AI_BASE_URL": "https://provider.example/v1"}[k]
		}, &out, &stderr,
		testTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "EXTERNAL_SOURCE_TITLE") {
				t.Error("generator ignored external source")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"完整摘要"}}]}`))}, nil
		}))
	var artifact struct {
		SuiteVersion  string `json:"suite_version"`
		SuiteHash     string `json:"suite_sha256"`
		SuiteFileHash string `json:"suite_file_sha256"`
		Status        string `json:"status"`
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	if code != 2 || calls != 2 || artifact.Status != "ungraded" || artifact.SuiteVersion != suite.Version || artifact.SuiteHash != suite.Hash() || artifact.SuiteFileHash != hash {
		t.Fatalf("external source binding missing: code=%d calls=%d artifact=%+v stderr=%s", code, calls, artifact, stderr.String())
	}
}

func TestCLIExternalControlsAndRubricsAreUsedForCalibrationAndGrading(t *testing.T) {
	path, _, suite := externalSuite(t)
	for i := range suite.Controls {
		suite.Controls[i].ID = "external-" + suite.Controls[i].ID
		suite.Controls[i].Output = summaryeval.Output{Brief: "external-control-" + string(suite.Controls[i].Expected), Detailed: "external-control-detail"}
	}
	for i := range suite.Cases {
		for j := range suite.Cases[i].Criteria {
			suite.Cases[i].Criteria[j].Expected += " EXTERNAL_RUBRIC_ONLY"
		}
	}
	raw, _ := json.Marshal(suite)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	outPath := filepath.Join(t.TempDir(), "external-graded.json")
	var stdout, stderr bytes.Buffer
	calls, judges := 0, 0
	code := execute(context.Background(), []string{"--live", "--grade", "--max-calls", "10", "--cases", suite.Cases[0].ID, "--suite-file", path, "--suite-sha256", hex.EncodeToString(h[:]), "--out", outPath},
		func(k string) string {
			return map[string]string{"AI_API_KEY": "test-secret", "AI_BASE_URL": "https://provider.example/v1"}[k]
		}, &stdout, &stderr,
		testTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			message := req.Messages[len(req.Messages)-1].Content
			answer := "fresh generated output"
			if _, data, ok := strings.Cut(message, "INPUT:\n"); ok {
				judges++
				var input struct {
					Criteria []summaryeval.Criterion `json:"criteria"`
					Output   summaryeval.Output      `json:"candidate_summary"`
				}
				if err := json.Unmarshal([]byte(data), &input); err != nil {
					t.Fatal(err)
				}
				judgment := summaryeval.Judgment{SchemaVersion: 1}
				for _, criterion := range input.Criteria {
					if !strings.Contains(criterion.Expected, "EXTERNAL_RUBRIC_ONLY") {
						t.Error("judge used embedded rubric")
					}
					verdict := summaryeval.Pass
					if input.Output.Brief == "external-control-fail" {
						verdict = summaryeval.Fail
					}
					judgment.Criteria = append(judgment.Criteria, summaryeval.CriterionVerdict{ID: criterion.ID, Verdict: verdict, SourceQuote: criterion.SourceEvidence, SummaryQuote: input.Output.Brief, Reason: "synthetic source-binding test, not semantic acceptance"})
				}
				b, _ := json.Marshal(judgment)
				answer = string(b)
			} else if strings.Contains(message, "EXTERNAL_RUBRIC_ONLY") || strings.Contains(message, "external-control-") {
				t.Error("review-only data leaked into generation")
			}
			b, _ := json.Marshal(answer)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":` + string(b) + `}}]}`))}, nil
		}))
	var artifact summaryeval.Artifact
	saved, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(saved, &artifact); err != nil {
		t.Fatal(err)
	}
	if code != 0 || calls != 10 || judges != 8 || artifact.Status != "graded_pass" || len(artifact.Calibration) != len(suite.Controls) {
		t.Fatalf("external grading binding failed code=%d calls=%d judges=%d stderr=%s", code, calls, judges, stderr.String())
	}
	for _, control := range artifact.Calibration {
		if !strings.HasPrefix(control.ControlID, "external-") {
			t.Fatal("calibration used embedded controls")
		}
	}
}

func TestCLIInvalidSuiteContentsRejectBeforeCredentialRead(t *testing.T) {
	path, _, _ := externalSuite(t)
	for _, raw := range [][]byte{[]byte(`{"schema_version":1,"version":"\ud800"}`), []byte(`{"schema_version":1,"schema_version":1}`), {0xff}, []byte(strings.Repeat(" ", (2<<20)+1))} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(raw)
		var out, stderr bytes.Buffer
		outPath := filepath.Join(t.TempDir(), "must-not-exist.json")
		code := execute(context.Background(), []string{"--live", "--max-calls", "1", "--out", outPath, "--suite-file", path, "--suite-sha256", hex.EncodeToString(h[:])}, func(k string) string {
			if strings.Contains(k, "KEY") {
				t.Fatal("invalid JSON read credentials")
			}
			return ""
		}, &out, &stderr, testTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid JSON made request"); return nil, nil }))
		if code != 1 {
			t.Fatalf("invalid JSON accepted %d %s", code, stderr.String())
		}
		if _, err := os.Lstat(outPath); !os.IsNotExist(err) {
			t.Fatal("invalid JSON created artifact")
		}
	}
}

// Narrow guard port from 88e316b; only the executing binary may attest itself.
func TestExpectedRevisionRequiresExactCleanEmbeddedSource(t *testing.T) {
	revision := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		info *debug.BuildInfo
		want bool
	}{
		{"clean", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "false"}}}, true},
		{"dirty", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "true"}}}, false},
		{"unknown dirty", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}}, false},
		{"different binary", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: strings.Repeat("b", 40)}, {Key: "vcs.modified", Value: "false"}}}, false},
		{"missing embedded metadata", nil, false},
		{"empty embedded metadata", &debug.BuildInfo{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkExpectedRevision(revision, tc.info); (err == nil) != tc.want {
				t.Fatalf("wrong guard result: %v", err)
			}
		})
	}
	for _, bad := range []string{"", "short", strings.Repeat("z", 40)} {
		if checkExpectedRevision(bad, &debug.BuildInfo{}) == nil {
			t.Fatal("invalid expected revision accepted")
		}
	}
	clean := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "false"}}}
	if err := checkExpectedRevision(strings.ToUpper(revision), clean); err != nil {
		t.Fatal("uppercase expected hex should canonicalize", err)
	}
}

func TestCLIRevisionGuardFailsBeforeCredentialsRequestsAndArtifact(t *testing.T) {
	for _, revision := range []string{"", "invalid", strings.Repeat("0", 40)} {
		path := filepath.Join(t.TempDir(), "revision-bound.json")
		var out, stderr bytes.Buffer
		code := execute(context.Background(), []string{"--live", "--max-calls", "1", "--out", path, "--expected-revision", revision}, func(k string) string {
			if strings.Contains(k, "KEY") {
				t.Fatal("rejected revision read credentials")
			}
			return ""
		}, &out, &stderr, testTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("rejected revision made request")
			return nil, nil
		}))
		if code != 1 {
			t.Fatalf("guard did not reject: %d %s", code, stderr.String())
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("rejected revision created artifact")
		}
	}
	var out, stderr bytes.Buffer
	if code := execute(context.Background(), []string{"--validate", "--expected-revision", strings.Repeat("a", 40)}, func(string) string { return "" }, &out, &stderr, nil); code != 1 {
		t.Fatal("offline revision guard silently ignored")
	}
}
