package api

import (
	"strings"
	"testing"
	"time"
)

const rewriteTestToken = "v1_0123456789abcdef0123456789abcdef_signature"

func TestShareAssetRewriteContinuesAfterUnclosedInlineCode(t *testing.T) {
	const articleID = 42
	local := "/api/articles/42/images/3.png"
	wantAsset := "/api/share/" + rewriteTestToken + "/assets/3.png"
	for name, prefix := range map[string]string{
		"single backtick":    "`unclosed ",
		"multiple backticks": "prefix ``unclosed ",
	} {
		t.Run(name, func(t *testing.T) {
			input := prefix + "![markdown](" + local + ") <img src=\"" + local + "\">"
			got := rewriteShareAssets(input, rewriteTestToken, articleID)
			if strings.Count(got, wantAsset) != 2 {
				t.Fatalf("rewritten assets=%d want=2: %s", strings.Count(got, wantAsset), got)
			}
		})
	}
}

func TestShareAssetRewriteMalformedInputStaysLinearAndDoesNotInjectToken(t *testing.T) {
	input := strings.Repeat("<img ", 20_000) + strings.Repeat("![", 20_000)
	done := make(chan string, 1)
	go func() {
		done <- rewriteShareAssets(input, rewriteTestToken, 42)
	}()
	select {
	case got := <-done:
		if got != input {
			t.Fatal("malformed input changed")
		}
		if strings.Contains(got, rewriteTestToken) {
			t.Fatal("share token injected into malformed input")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("malformed input rewrite exceeded 500ms; likely superlinear")
	}
}
