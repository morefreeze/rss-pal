package repository

import (
	"errors"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func art(id int) model.Article { return model.Article{ID: id} }

func TestCombineLinkSetResults_PrimaryFull(t *testing.T) {
	primary := []model.Article{art(1), art(2), art(3)}
	fallbackCalled := false
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		fallbackCalled = true
		return nil, nil
	}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fallbackCalled {
		t.Errorf("fallback should not be called when primary >= limit")
	}
	if len(got) != 3 {
		t.Fatalf("want 3 articles, got %d", len(got))
	}
	for i, a := range got {
		if a.IsFallback {
			t.Errorf("got[%d].IsFallback = true, want false", i)
		}
	}
}

func TestCombineLinkSetResults_PrimaryEmptyFallbackFills(t *testing.T) {
	primary := []model.Article{}
	fallback := []model.Article{art(10), art(11), art(12)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return fallback, nil
	}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 articles, got %d", len(got))
	}
	for i, a := range got {
		if !a.IsFallback {
			t.Errorf("got[%d].IsFallback = false, want true (id=%d)", i, a.ID)
		}
	}
}

func TestCombineLinkSetResults_PartialPrimaryPlusFallback(t *testing.T) {
	primary := []model.Article{art(1), art(2)}
	fallback := []model.Article{art(20), art(21), art(22)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return fallback, nil
	}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 articles, got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("primary order corrupted: got ids %d, %d", got[0].ID, got[1].ID)
	}
	if got[0].IsFallback || got[1].IsFallback {
		t.Errorf("primary articles should have IsFallback=false")
	}
	for i := 2; i < 5; i++ {
		if !got[i].IsFallback {
			t.Errorf("got[%d].IsFallback = false, want true", i)
		}
	}
}

func TestCombineLinkSetResults_FallbackErrorReturnsPrimary(t *testing.T) {
	primary := []model.Article{art(1)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return nil, errors.New("simulated fallback failure")
	}, 5)
	if err != nil {
		t.Fatalf("fallback error should be swallowed, got %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("want only primary article (id=1), got %v", got)
	}
	if got[0].IsFallback {
		t.Errorf("primary article should not be marked fallback")
	}
}
