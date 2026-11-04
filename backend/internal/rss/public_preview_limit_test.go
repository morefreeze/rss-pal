package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Use a fresh process because net/http caches ProxyFromEnvironment settings.
// The production public client sees a public IP literal, while its operator
// proxy is this loopback fixture. No public IP is contacted or DNS queried.
func TestPublicPreviewRejectsFeedBeyondEightMiB(t *testing.T) {
	const fixtureMode = "RSS_PAL_PREVIEW_LIMIT_FIXTURE"
	if os.Getenv(fixtureMode) == "1" {
		fetcher := NewPublicFetcher("")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		preview, err := fetcher.Preview(ctx, "http://93.184.216.34/small.xml")
		if err != nil || preview == nil || preview.FeedType != "rss" || len(preview.Items) != 1 {
			t.Fatalf("small controlled feed rejected: preview=%v err=%v", preview, err)
		}
		preview, err = fetcher.Preview(ctx, "http://93.184.216.34/oversize.xml")
		if err == nil || preview != nil || !strings.Contains(err.Error(), "response exceeds 8388608 bytes") {
			t.Fatalf("oversize public preview escaped the new bound: preview=%v err=%v", preview, err)
		}
		return
	}

	prefix := `<?xml version="1.0"?><rss version="2.0"><channel><title>Size fixture</title>` +
		`<link>https://example.invalid</link><description>fixture</description>` +
		`<item><title>Post</title><link>https://example.invalid/post</link><description><![CDATA[`
	suffix := `]]></description></item></channel></rss>`
	oversize := prefix + strings.Repeat("x", (8<<20)+1) + suffix
	if _, err := NewFetcher("").parser.ParseString(oversize); err != nil {
		t.Fatalf("oversize control must be a valid RSS feed: %v", err)
	}
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		// For absolute-form proxy requests Go's server derives req.Host from
		// that absolute URL, including the transport's explicit default port.
		if req.URL.Host != "93.184.216.34:80" {
			t.Errorf("fixture received unexpected pinned target: URL=%s", req.URL)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.(http.Flusher).Flush() // Exercise the read cap, not a Content-Length precheck.
		if req.URL.Path == "/oversize.xml" {
			_, _ = w.Write([]byte(oversize))
		} else if req.URL.Path == "/small.xml" {
			_, _ = w.Write([]byte(prefix + "small" + suffix))
		} else {
			t.Errorf("fixture received unexpected path %s", req.URL.Path)
		}
	}))
	defer proxy.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestPublicPreviewRejectsFeedBeyondEightMiB$", "-test.v")
	for _, entry := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		switch name {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", fixtureMode:
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.Env = append(command.Env, fixtureMode+"=1", "HTTP_PROXY="+proxy.URL, "HTTPS_PROXY="+proxy.URL, "NO_PROXY=")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("controlled public-client process: %v\n%s", err, output)
	}
	if requests.Load() != 2 {
		t.Fatalf("expected both previews through controlled proxy, received %d", requests.Load())
	}
	t.Logf("production public client: small RSS accepted, valid RSS >8 MiB rejected\n%s", output)
}
