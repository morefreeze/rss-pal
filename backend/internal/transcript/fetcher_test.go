package transcript

import (
	"context"
	"errors"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

type stubFetcher struct {
	r   *Result
	err error
}

func (s *stubFetcher) Fetch(ctx context.Context, _ *model.Article) (*Result, error) {
	return s.r, s.err
}

func TestMultiFetcher_FirstHitWins(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{},
		&stubFetcher{r: &Result{Text: "second", Source: "stub2"}},
		&stubFetcher{r: &Result{Text: "third", Source: "stub3"}},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got == nil || got.Text != "second" {
		t.Errorf("expected second to win, got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_AllNil(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{&stubFetcher{}, &stubFetcher{}}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil), got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_ErrorThenSuccess(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{err: errors.New("transient")},
		&stubFetcher{r: &Result{Text: "ok", Source: "stub"}},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got == nil || got.Text != "ok" {
		t.Errorf("expected success after transient err, got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_AllErrorsReturnFirst(t *testing.T) {
	first := errors.New("first")
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{err: first},
		&stubFetcher{err: errors.New("second")},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if got != nil || !errors.Is(err, first) {
		t.Errorf("expected first error, got (%+v, %v)", got, err)
	}
}
