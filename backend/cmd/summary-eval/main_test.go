package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestValidationIsOfflineAndDoesNotClaimSemanticAcceptance(t *testing.T) {
	var out, stderr bytes.Buffer
	code := execute(context.Background(), []string{"--validate"}, func(string) string { return "" }, &out, &stderr,
		testTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("schema validation must be offline")
			return nil, nil
		}))
	if code != 0 || !strings.Contains(out.String(), "9 cases") || !strings.Contains(out.String(), "semantic evaluation not run") || strings.Contains(out.String(), "graded_pass") {
		t.Fatalf("unexpected validation: %d %s %s", code, out.String(), stderr.String())
	}
}

func TestCLIRequiresLiveFlagBudgetAndNewArtifactPath(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--grade"},
		{"--live", "--max-calls", "2"},
		{"--live", "--out", "unused.json"},
		{"--validate", "--live"},
		{"--live", "--max-calls", "2", "--out", "unused.json", "--timeout", "0s"},
	} {
		var out, stderr bytes.Buffer
		code := execute(context.Background(), args, func(string) string { return "" }, &out, &stderr,
			testTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid CLI invocation must not make requests")
				return nil, nil
			}))
		if code != 1 || strings.Contains(stderr.String(), "secret-key") {
			t.Fatalf("unexpected CLI rejection: %d %s %s", code, out.String(), stderr.String())
		}
	}
}

func TestCLISavesIncompleteRunAndReturnsNonzeroWithoutExposingSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.json")
	var out, stderr bytes.Buffer
	var calls int
	env := map[string]string{"AI_API_KEY": "secret-key", "AI_BASE_URL": "https://provider.example/v1", "AI_MODEL": "test-model"}
	code := execute(context.Background(), []string{"--live", "--max-calls", "1", "--cases", "017", "--out", path},
		func(k string) string { return env[k] }, &out, &stderr, testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"length","message":{"content":"secret-key"}}]}`))}, nil
		}))
	b, err := os.ReadFile(path)
	if code != 2 || err != nil || calls != 1 || !strings.Contains(string(b), `"status": "incomplete"`) {
		t.Fatalf("expected durable incomplete artifact: %d %v %s %s", code, err, out.String(), stderr.String())
	}
	if strings.Contains(out.String()+stderr.String()+string(b), "secret-key") {
		t.Fatal("credential escaped to output or disk")
	}
}

func TestCompiledRevisionFallbackPreservesRevisionAndDirtyEvidence(t *testing.T) {
	revision := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
		want     string
		dirty    bool
	}{
		{"clean compiled source", []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "false"}}, revision, false},
		{"modified source", []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: "true"}}, revision, true},
		{"unspecified dirty state", []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}, revision, true},
		{"invalid revision", []debug.BuildSetting{{Key: "vcs.revision", Value: "not-a-sha"}}, "unknown", true},
		{"nonhex revision", []debug.BuildSetting{{Key: "vcs.revision", Value: strings.Repeat("z", 40)}}, "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, dirty := buildRevision(&debug.BuildInfo{Settings: tc.settings})
			if got != tc.want || dirty != tc.dirty {
				t.Fatalf("wrong compiled provenance: %q/%t", got, dirty)
			}
		})
	}
}

func TestCompiledRevisionWinsOverDifferentAmbientCheckout(t *testing.T) {
	compiled, ambient := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, modified := range []bool{false, true} {
		info := &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: compiled},
			{Key: "vcs.modified", Value: map[bool]string{false: "false", true: "true"}[modified]},
		}}
		var gitCalls int
		got, dirty := resolveSourceRevision(info, func(args ...string) ([]byte, error) {
			gitCalls++
			if args[0] == "rev-parse" {
				return []byte(ambient + "\n"), nil
			}
			return nil, nil // the ambient checkout is clean
		})
		if got != compiled || dirty != modified || gitCalls != 0 {
			t.Fatalf("binary provenance replaced by ambient Git: got=%s dirty=%t git_calls=%d", got, dirty, gitCalls)
		}
	}
}

func TestGitRevisionFallbackWhenBuildHasNoUsableVCSRevision(t *testing.T) {
	ambient := strings.Repeat("b", 40)
	for _, info := range []*debug.BuildInfo{nil, {}, {Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "invalid"}}}} {
		got, dirty := resolveSourceRevision(info, func(args ...string) ([]byte, error) {
			if args[0] == "rev-parse" {
				return []byte(ambient), nil
			}
			return []byte(" M file.go\n"), nil
		})
		if got != ambient || !dirty {
			t.Fatalf("missing/invalid embedded VCS must fall back to Git: %s/%t", got, dirty)
		}
	}
	got, dirty := resolveSourceRevision(nil, func(...string) ([]byte, error) { return nil, errors.New("git unavailable") })
	if got != "unknown" || !dirty {
		t.Fatalf("unavailable provenance must remain unknown: %s/%t", got, dirty)
	}
}
