package rss

import (
	"strings"
	"testing"
)

func TestPlainExcerpt(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`<p>Hello &amp; world</p><p>second<script>evil()</script><style>hide</style></p>`, `Hello & world second`},
		{"Compare a < b and `<div>` with <Type>", "Compare a < b and `<div>` with <Type>"},
		{`&lt;p&gt;decoded&lt;/p&gt;`, `decoded`},
		{"<p>RSSPALCODE1END `<div>`</p>", "RSSPALCODE1END `<div>`"},
		{`<p>Use <code>&lt;div&gt;example&lt;/div&gt;</code></p>`, `Use <div>example</div>`},
	} {
		if got := PlainExcerpt(tc.raw); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestReaderURLPreservesTailCodeTableAndLazyImage(t *testing.T) {
	raw := `<article><p>` + strings.Repeat("Opening paragraph. ", 10) + `</p><pre><code>if (a &lt; b) return 1;</code></pre><table><tr><th>key</th><th>value</th></tr><tr><td>tailkey</td><td>tailvalue</td></tr></table><img data-src="/image.png" alt="diagram"><p><a href="/conclusion">Conclusion tail</a></p></article>`
	result, err := (&ContentFetcher{}).FetchContentWithMetadataFromReaderURL(strings.NewReader(raw), "https://8.8.8.8/post")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"if (a < b)", "tailkey", "tailvalue", "https://8.8.8.8/image.png", "https://8.8.8.8/conclusion", "Conclusion tail"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("missing %q in %s", want, result.Content)
		}
	}
}
