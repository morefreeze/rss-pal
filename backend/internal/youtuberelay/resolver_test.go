package youtuberelay

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeCommandRunner struct {
	name   string
	args   []string
	output []byte
	err    error
	wait   bool
	calls  int
}

func (r *fakeCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls++
	r.name = name
	r.args = append([]string(nil), args...)
	if r.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.output, r.err
}

func resolvedInfoJSON(t *testing.T) []byte {
	t.Helper()
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 212, Formats: []Format{
		{ID: "137", URL: googleURL("v137"), Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 3500},
		{ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128},
	}}
	body, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestYTDLPResolverUsesSafeDeterministicArguments(t *testing.T) {
	runner := &fakeCommandRunner{output: resolvedInfoJSON(t)}
	resolver := YTDLPResolver{Runner: runner, Binary: "yt-dlp", Timeout: time.Second}

	resolved, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Info.ID != "dQw4w9WgXcQ" || resolved.Selection.Quality != 1080 {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
	wantArgs := []string{
		"--no-warnings",
		"--no-playlist",
		"--skip-download",
		"--socket-timeout", "20",
		"--js-runtimes", "deno",
		"-J",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	if runner.name != "yt-dlp" || !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("command = %s %q, want yt-dlp %q", runner.name, runner.args, wantArgs)
	}
}

func TestYTDLPResolverUsesConfiguredPOTProvider(t *testing.T) {
	runner := &fakeCommandRunner{output: resolvedInfoJSON(t)}
	resolver := YTDLPResolver{
		Runner:         runner,
		Binary:         "yt-dlp",
		Timeout:        time.Second,
		POTProviderURL: "http://youtube-pot:4416",
	}

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"--no-warnings",
		"--no-playlist",
		"--skip-download",
		"--socket-timeout", "20",
		"--js-runtimes", "deno",
		"--extractor-args", "youtube:player_client=mweb",
		"--extractor-args", "youtubepot-bgutilhttp:base_url=http://youtube-pot:4416",
		"-J",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("args = %q, want %q", runner.args, wantArgs)
	}
}

func TestNewYTDLPResolverReadsPOTProviderURL(t *testing.T) {
	t.Setenv("YOUTUBE_POT_PROVIDER_URL", "http://youtube-pot:4416")

	resolver := NewYTDLPResolver()

	if resolver.POTProviderURL != "http://youtube-pot:4416" {
		t.Fatalf("POTProviderURL = %q", resolver.POTProviderURL)
	}
}

func TestYTDLPResolverRejectsInvalidIDBeforeCommand(t *testing.T) {
	runner := &fakeCommandRunner{output: resolvedInfoJSON(t)}
	resolver := YTDLPResolver{Runner: runner, Timeout: time.Second}

	if _, err := resolver.Resolve(context.Background(), "../unsafe"); !errors.Is(err, ErrInvalidVideoID) {
		t.Fatalf("error = %v, want ErrInvalidVideoID", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestYTDLPResolverRejectsMismatchedID(t *testing.T) {
	body := resolvedInfoJSON(t)
	var info VideoInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatal(err)
	}
	info.ID = "aaaaaaaaaaa"
	body, _ = json.Marshal(info)
	resolver := YTDLPResolver{Runner: &fakeCommandRunner{output: body}, Timeout: time.Second}

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); !errors.Is(err, ErrResolveFailed) {
		t.Fatalf("error = %v, want ErrResolveFailed", err)
	}
}

func TestYTDLPResolverReportsMalformedJSONAndCommandFailure(t *testing.T) {
	cases := []struct {
		name   string
		runner *fakeCommandRunner
	}{
		{name: "malformed json", runner: &fakeCommandRunner{output: []byte("{")}},
		{name: "command failure", runner: &fakeCommandRunner{err: errors.New("exit status 1")}},
		{name: "oversized output", runner: &fakeCommandRunner{output: make([]byte, maxResolverOutput+1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := YTDLPResolver{Runner: tc.runner, Timeout: time.Second}
			if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); !errors.Is(err, ErrResolveFailed) {
				t.Fatalf("error = %v, want ErrResolveFailed", err)
			}
		})
	}
}

func TestYTDLPResolverHonorsTimeout(t *testing.T) {
	runner := &fakeCommandRunner{wait: true}
	resolver := YTDLPResolver{Runner: runner, Timeout: 20 * time.Millisecond}
	start := time.Now()

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); !errors.Is(err, ErrResolveFailed) {
		t.Fatalf("error = %v, want ErrResolveFailed", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("resolver took %v, want prompt timeout", elapsed)
	}
}

func TestYTDLPResolverRejectsFormatsOutsideGoogleVideo(t *testing.T) {
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 100, Formats: []Format{
		{ID: "137", URL: "https://example.com/video.mp4", Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 2000},
		{ID: "140", URL: "https://example.com/audio.m4a", Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128},
	}}
	body, _ := json.Marshal(info)
	resolver := YTDLPResolver{Runner: &fakeCommandRunner{output: body}, Timeout: time.Second}

	if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); !errors.Is(err, ErrNoCompatibleMedia) {
		t.Fatalf("error = %v, want ErrNoCompatibleMedia", err)
	}
}
