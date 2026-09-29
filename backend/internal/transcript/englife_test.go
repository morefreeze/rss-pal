package transcript

import (
	"context"
	"github.com/bytedance/rss-pal/internal/model"
	"testing"
	"time"
)

type fallbackService struct {
	connected bool
	live      bool
}

func (s *fallbackService) Connected(context.Context) (bool, error) { return s.connected, nil }
func (s *fallbackService) Fetch(ctx context.Context, _ string) (string, error) {
	s.live = ctx.Err() == nil
	return "generated content", ctx.Err()
}

type contextFetcher struct {
	original context.Context
	same     bool
	block    bool
}

func (f *contextFetcher) Fetch(ctx context.Context, _ *model.Article) (*Result, error) {
	f.same = ctx == f.original
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, nil
}
func TestEnglifeFallbackReservesLiveParentContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	primary := &contextFetcher{original: ctx, block: true}
	service := &fallbackService{connected: true}
	f := &EnglifeFallback{Primary: primary, Service: service, PrimaryTimeout: 10 * time.Millisecond, ReservedTime: 500 * time.Millisecond}
	r, err := f.Fetch(ctx, &model.Article{MediaType: "video/youtube", URL: "https://www.youtube.com/watch?v=abcdefghijk"})
	if err != nil || r == nil || !service.live || primary.same {
		t.Fatalf("fallback had no time: %v", err)
	}
}
func TestEnglifeDisconnectedPreservesPrimaryDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p := &contextFetcher{original: ctx}
	f := &EnglifeFallback{Primary: p, Service: &fallbackService{}}
	_, _ = f.Fetch(ctx, &model.Article{MediaType: "video/youtube"})
	if !p.same {
		t.Fatal("changed primary behavior while disconnected")
	}
}
