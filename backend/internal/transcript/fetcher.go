package transcript

import (
	"context"

	"github.com/bytedance/rss-pal/internal/model"
)

// Result is what a successful Fetcher returns. Text is the transcript as
// plain text or simple markdown paragraphs (no embedded timestamps unless
// they're part of the transcript itself, which is rare). Source is a short
// human-readable label, e.g. "YouTube CC" or "bbc.co.uk 网页字幕".
type Result struct {
	Text   string
	Source string
}

// Fetcher returns a transcript for the given article. The contract is:
//
//   - (Result, nil)  — transcript found.
//   - (nil, nil)     — no transcript exists for this article (do not retry).
//   - (nil, err)     — transient failure (network, parse). Caller may retry
//                      next cycle. Distinct from "no transcript".
//
// Fetchers should not panic on malformed input. They should also be cheap
// to invoke when they don't apply (e.g. YouTubeCC on a Bilibili article
// returns (nil, nil) immediately based on media_type).
type Fetcher interface {
	Fetch(ctx context.Context, article *model.Article) (*Result, error)
}

// MultiFetcher tries each strategy in order and returns the first non-nil
// Result. A transient error from one strategy does NOT abort: the next
// strategy still gets a chance. Errors are coalesced — if every strategy
// errored and none produced a Result, the first error is returned.
type MultiFetcher struct {
	Strategies []Fetcher
}

func (m *MultiFetcher) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	var firstErr error
	for _, s := range m.Strategies {
		r, err := s.Fetch(ctx, article)
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		if r != nil && r.Text != "" {
			return r, nil
		}
	}
	return nil, firstErr
}
