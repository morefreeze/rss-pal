package transcript

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/model"
	"time"
)

// Englife is a last-resort provider of a generated reading article, not captions.
type Englife struct {
	Service interface {
		Fetch(context.Context, string) (string, error)
	}
}

func (f *Englife) Fetch(ctx context.Context, a *model.Article) (*Result, error) {
	if f.Service == nil || a == nil || a.MediaType != "video/youtube" {
		return nil, nil
	}
	id := extractYouTubeID(a)
	if id == "" {
		return nil, nil
	}
	text, err := f.Service.Fetch(ctx, id)
	if err != nil || text == "" {
		return nil, err
	}
	return &Result{Text: text, Source: "englife", Kind: "article"}, nil
}
func (r *Result) Heading() string {
	if r.Kind == "article" {
		return "视频整理（englife）"
	}
	return "字幕"
}

// EnglifeFallback reserves part of the caller's deadline for the connected
// provider. A slow yt-dlp process must not consume the entire worker cycle.
type EnglifeFallback struct {
	Primary Fetcher
	Service interface {
		Fetch(context.Context, string) (string, error)
		Connected(context.Context) (bool, error)
	}
	PrimaryTimeout time.Duration
	ReservedTime   time.Duration
}

func (f *EnglifeFallback) Fetch(ctx context.Context, a *model.Article) (*Result, error) {
	if a == nil || a.MediaType != "video/youtube" || f.Service == nil {
		return f.Primary.Fetch(ctx, a)
	}
	connected, connectionErr := f.Service.Connected(ctx)
	if !connected || connectionErr != nil {
		r, err := f.Primary.Fetch(ctx, a)
		if r != nil {
			return r, err
		}
		return nil, errors.Join(err, connectionErr)
	}
	budget := f.PrimaryTimeout
	if budget <= 0 {
		budget = 45 * time.Second
	}
	reserved := f.ReservedTime
	if reserved <= 0 {
		reserved = 45 * time.Second
	}
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline) - reserved; left < budget {
			budget = left
		}
	}
	var primaryErr error
	if budget > 0 {
		child, cancel := context.WithTimeout(ctx, budget)
		result, err := f.Primary.Fetch(child, a)
		cancel()
		if result != nil && result.Text != "" {
			return result, nil
		}
		primaryErr = err
	}
	fallback := &Englife{Service: f.Service}
	result, err := fallback.Fetch(ctx, a)
	if result != nil {
		return result, err
	}
	return nil, errors.Join(primaryErr, err)
}
