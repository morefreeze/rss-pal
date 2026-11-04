package rss

import (
	"os"
	"strings"
	"testing"
)

func TestLilianWengSourceExcerptsSurviveExtraction(t *testing.T) {
	raw, err := os.ReadFile("testdata/lilianweng-source-excerpts.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, pipeline := range []string{"feed", "page"} {
		t.Run(pipeline, func(t *testing.T) {
			got := NormalizeFeedContent(string(raw), "https://lilianweng.github.io/")
			if pipeline == "page" {
				got, err = NewContentFetcher().FetchContentFromReader(strings.NewReader(string(raw)))
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, want := range []string{
				`$f(x) \approx x$`, `$h_i \to h_{i+1}$`,
				`[games](https://youtu.be/vFr3K2DORc8)`,
				`[Nuts and Bolts of Applying Deep Learning](https://youtu.be/F1ka6a13S9I)`,
				`[https://youtu.be/F1ka6a13S9I](https://youtu.be/F1ka6a13S9I)`,
				`[Godfather of deep learning!](https://youtu.be/uAu3jQWaN6E)`,
				`[“Information Theory in Deep Learning”](https://youtu.be/bLqJHjXihK8)`,
				`[nice video](https://youtu.be/P1A1yNsxMjc)`,
				`[Tishby’ talk 15:15](https://youtu.be/bLqJHjXihK8?t=15m15s)`,
				`[Tishby’ talk 24:50](https://youtu.be/bLqJHjXihK8?t=24m56s)`,
				`[Information Theory of Deep Learning](https://youtu.be/bLqJHjXihK8)`,
			} {
				if !strings.Contains(got, want) {
					t.Errorf("source semantics missing %q in %q", want, got)
				}
			}
		})
	}
}
