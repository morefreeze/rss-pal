// summary-eval is an experiment runner. It never connects to an article DB.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/summaryeval"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(execute(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, nil))
}

func execute(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, base http.RoundTripper) int {
	f := flag.NewFlagSet("summary-eval", flag.ContinueOnError)
	// flag errors can repeat arbitrary argument values. Never print those values.
	f.SetOutput(io.Discard)
	validate := f.Bool("validate", false, "validate frozen suite offline; no semantic acceptance")
	live := f.Bool("live", false, "explicitly allow bounded provider requests")
	grade := f.Bool("grade", false, "also run calibrated semantic model grading")
	suiteFile := f.String("suite-file", "", "explicit frozen suite JSON file; never modifies the embedded default")
	suiteSHA := f.String("suite-sha256", "", "expected SHA-256 of exact suite file bytes; required with --suite-file for --live")
	var expectedRevision string
	// Narrow opt-in guard ported from 88e316b; no serving behavior is imported.
	f.Func("expected-revision", "require a clean executing binary built at this 40-hex revision", func(value string) error {
		value = strings.ToLower(value)
		if !validRevision(value) {
			return fmt.Errorf("expected revision must be 40 hexadecimal characters")
		}
		expectedRevision = value
		return nil
	})
	maxCalls := f.Int("max-calls", 0, "required hard cap on physical requests, including retries and judges (1–256)")
	out := f.String("out", "", "new private JSON artifact path; existing files are never overwritten")
	model := f.String("model", envOr(getenv, "AI_MODEL", ai.DefaultModel), "pinned generator model ID (or AI_MODEL)")
	baseURL := f.String("base-url", getenv("AI_BASE_URL"), "OpenAI-compatible base URL (or AI_BASE_URL)")
	judgeModel := f.String("judge-model", getenv("SUMMARY_EVAL_JUDGE_MODEL"), "optional separate grader model")
	judgeBase := f.String("judge-base-url", getenv("SUMMARY_EVAL_JUDGE_BASE_URL"), "optional separate grader endpoint")
	cases := f.String("cases", "", "comma-separated case IDs; default all cases in the selected suite")
	split := f.String("split", "all", "all, train, or held_out")
	routes := f.String("routes", "default", "comma-separated default, stream, custom, custom-stream, vision, vision-stream")
	images := f.String("images", "", "explicit source JPEG image paths for vision routes; comma-separated")
	briefFile := f.String("brief-template-file", "", "custom brief template UTF-8 file ({title}/{content})")
	detailedFile := f.String("detailed-template-file", "", "custom detailed template UTF-8 file ({title}/{content})")
	timeout := f.Duration("timeout", 15*time.Minute, "total run deadline; interrupted runs are saved as incomplete")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, "summary-eval --validate [--suite-file FILE] | --live --max-calls N --out NEW.json [--suite-file FILE --suite-sha256 SHA] [--expected-revision SHA] [--grade] [--cases IDs] [--routes ROUTES]")
			fmt.Fprintln(stdout, "Credentials: AI_API_KEY; optional SUMMARY_EVAL_JUDGE_API_KEY. No key flags or implicit .env loading.")
			return 0
		}
		fmt.Fprintln(stderr, "Invalid flags. Use --help; values omitted.")
		return 1
	}
	fileSet, shaSet := false, false
	f.Visit(func(value *flag.Flag) {
		fileSet = fileSet || value.Name == "suite-file"
		shaSet = shaSet || value.Name == "suite-sha256"
	})
	if f.NArg() != 0 || *validate && (*live || *grade || expectedRevision != "") {
		fmt.Fprintln(stderr, "Use either --validate or an explicitly bounded --live run.")
		return 1
	}
	if fileSet && *suiteFile == "" || shaSet && (!fileSet || *suiteSHA == "") || *live && fileSet && !shaSet {
		fmt.Fprintln(stderr, "A live --suite-file requires --suite-sha256; checksum flags require a nonempty explicit suite file.")
		return 1
	}
	var suite summaryeval.Suite
	var suiteFileHash string
	var err error
	if fileSet {
		suite, suiteFileHash, err = summaryeval.LoadSuiteFile(*suiteFile, *suiteSHA)
	} else {
		suite, err = summaryeval.LoadSuite()
	}
	if err != nil {
		fmt.Fprintln(stderr, "Frozen suite rejected:", err)
		return 1
	}
	if *validate {
		fmt.Fprintf(stdout, "Fixture schema valid: %s, %d cases, %d controls; semantic evaluation not run.\n", suite.Version, len(suite.Cases), len(suite.Controls))
		fmt.Fprintf(stdout, "suite_sha256=%s\n", suite.Hash())
		if suiteFileHash != "" {
			fmt.Fprintf(stdout, "suite_file_sha256=%s\n", suiteFileHash)
		}
		return 0
	}
	if !*live || *maxCalls < 1 || *maxCalls > 256 || *out == "" || *timeout <= 0 {
		fmt.Fprintln(stderr, "Provider calls require --live, --max-calls 1–256, --out NEW.json, and a positive timeout.")
		return 1
	}
	if expectedRevision != "" {
		info, _ := debug.ReadBuildInfo()
		if err := checkExpectedRevision(expectedRevision, info); err != nil {
			fmt.Fprintln(stderr, "Expected revision guard rejected:", err)
			return 1
		}
	}
	if _, statErr := os.Lstat(*out); !os.IsNotExist(statErr) {
		fmt.Fprintln(stderr, "Artifact path must be new and accessible.")
		return 1
	}
	brief, err := readTemplate(*briefFile)
	if err != nil {
		fmt.Fprintln(stderr, "Unable to read bounded brief template.")
		return 1
	}
	detailed, err := readTemplate(*detailedFile)
	if err != nil {
		fmt.Fprintln(stderr, "Unable to read bounded detailed template.")
		return 1
	}
	revision, dirty := sourceRevision()
	cfg := summaryeval.Config{Live: *live, Grade: *grade, MaxCalls: *maxCalls, CaseIDs: csv(*cases), Split: *split,
		Generator:     summaryeval.Provider{Key: getenv("AI_API_KEY"), BaseURL: *baseURL, Model: *model},
		Judge:         summaryeval.Provider{Key: getenv("SUMMARY_EVAL_JUDGE_API_KEY"), BaseURL: *judgeBase, Model: *judgeModel},
		BriefTemplate: brief, DetailedTemplate: detailed, ImagePaths: csv(*images), Revision: revision, Dirty: dirty, SuiteFileHash: suiteFileHash}
	for _, route := range csv(*routes) {
		cfg.Routes = append(cfg.Routes, summaryeval.Route(route))
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	artifact, err := summaryeval.Run(ctx, cfg, suite, base)
	if err != nil {
		fmt.Fprintln(stderr, "Evaluation configuration rejected:", err)
		return 1
	}
	if err := summaryeval.SaveArtifact(*out, artifact, []string{cfg.Generator.Key, cfg.Judge.Key}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "status=%s results=%d/%d requests=%d/%d calibrated=%t human_review=required\n", artifact.Status, len(artifact.Results), artifact.PlannedResults, artifact.CallCount, artifact.MaxCalls, artifact.Calibrated)
	fmt.Fprintln(stdout, "Saved new private JSON artifact.")
	if artifact.Status == "graded_pass" {
		return 0
	}
	return 2
}

func envOr(getenv func(string) string, key, fallback string) string {
	if s := getenv(key); s != "" {
		return s
	}
	return fallback
}

func csv(value string) []string {
	var out []string
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func readTemplate(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(b) > 64<<10 || len(strings.TrimSpace(string(b))) == 0 {
		return "", fmt.Errorf("template must be nonempty and at most 64 KiB")
	}
	return string(b), nil
}

func sourceRevision() (string, bool) {
	// Only fixed read-only commands; never a shell or credential-bearing config.
	info, _ := debug.ReadBuildInfo()
	return resolveSourceRevision(info, func(args ...string) ([]byte, error) {
		return exec.Command("git", args...).Output()
	})
}

func resolveSourceRevision(info *debug.BuildInfo, git func(...string) ([]byte, error)) (string, bool) {
	// A deployed binary can run inside a different checkout. Its embedded VCS
	// settings describe the executing code; the current directory cannot replace them.
	if info != nil {
		if revision, dirty := buildRevision(info); revision != "unknown" {
			return revision, dirty
		}
	}
	b, err := git("rev-parse", "HEAD")
	if err != nil {
		return "unknown", true
	}
	revision := strings.TrimSpace(string(b))
	if !validRevision(revision) {
		return "unknown", true
	}
	b, err = git("status", "--porcelain")
	return revision, err != nil || len(b) != 0
}

func validRevision(revision string) bool {
	if len(revision) != 40 {
		return false
	}
	for _, r := range revision {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// This opt-in guard trusts only the executing binary's embedded source metadata.
// Ambient Git cannot attest the code in a previously compiled binary.
func checkExpectedRevision(expected string, info *debug.BuildInfo) error {
	expected = strings.ToLower(expected)
	if !validRevision(expected) {
		return fmt.Errorf("expected revision must be 40 hexadecimal characters")
	}
	if info == nil {
		return fmt.Errorf("executing binary has no embedded VCS revision")
	}
	revision, dirty := buildRevision(info)
	if revision == "unknown" {
		return fmt.Errorf("executing binary has no valid embedded VCS revision")
	}
	if dirty {
		return fmt.Errorf("embedded source is not clean")
	}
	if revision != expected {
		return fmt.Errorf("embedded source revision does not match the expected revision")
	}
	return nil
}

func buildRevision(info *debug.BuildInfo) (string, bool) {
	revision, dirty := "unknown", true
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && validRevision(setting.Value) {
			revision = setting.Value
		}
		if setting.Key == "vcs.modified" {
			dirty = setting.Value != "false"
		}
	}
	if revision == "unknown" {
		dirty = true
	}
	return revision, dirty
}
