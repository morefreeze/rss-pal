package service

import (
	"math"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestComputeValueScore_Normal(t *testing.T) {
	m := FeedMetrics{
		Exposures:       50,
		Clicks:          20,
		CompletedReads:  10,
		AvgDurationMin:  5.0,
		FeedbackDensity: 2.0,
	}
	got := ComputeValueScore(m)
	// CTR = 0.4, completion = 0.5, normDur = 0.5, normFeedback = 0.4
	// 0.35*0.4 + 0.35*0.5 + 0.20*0.5 + 0.10*0.4 = 0.14 + 0.175 + 0.10 + 0.04 = 0.455
	want := 0.455
	if !almostEqual(got, want, 0.001) {
		t.Errorf("ComputeValueScore = %f, want %f", got, want)
	}
}

func TestComputeValueScore_ColdStartReturnsNaN(t *testing.T) {
	m := FeedMetrics{Exposures: 5}
	got := ComputeValueScore(m)
	if !math.IsNaN(got) {
		t.Errorf("ComputeValueScore for cold start = %f, want NaN", got)
	}
}

func TestComputeValueScore_ZeroClicks(t *testing.T) {
	m := FeedMetrics{Exposures: 50, Clicks: 0}
	// ctr=0, completion=NaN handled→0, dur=0, feedback=0 → 0
	got := ComputeValueScore(m)
	if !almostEqual(got, 0.0, 0.001) {
		t.Errorf("ComputeValueScore zero clicks = %f, want 0", got)
	}
}

func TestPruningRule_FullyDead(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 0,
		ClicksLast90d:   0,
		ProducedLast30d: 0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R1" {
		t.Errorf("got %+v, want R1", rule)
	}
}

func TestPruningRule_Dormant(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   2, // not fully dead
		ProducedLast30d: 5,
		ClicksLast30d:   0, // dormant in 30d
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R2" {
		t.Errorf("got %+v, want R2", rule)
	}
}

func TestPruningRule_DormantBelowMinArticles(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   2,
		ProducedLast30d: 1, // below DormantMinArticles=3
		ClicksLast30d:   0,
	}
	// Falls through to R3 since ProducedLast30d=1 (not exactly 0) — actually
	// R3 needs 0. So should return nil (no rule).
	rule := EvaluatePruningRule(m, cfg)
	if rule != nil {
		t.Errorf("got %+v, want nil", rule)
	}
}

func TestPruningRule_DeadFeed(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 1, // not fully dead
		ClicksLast90d:   1,
		ProducedLast30d: 0, // dead source
		ClicksLast30d:   0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R3" {
		t.Errorf("got %+v, want R3", rule)
	}
}

func TestPruningRule_LowValue(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.05
	m := FeedMetrics{
		ProducedLast90d: 30,
		ClicksLast90d:   2,
		ProducedLast30d: 12,
		ClicksLast30d:   1,
		ValueScore:      &score,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R4" {
		t.Errorf("got %+v, want R4", rule)
	}
}

func TestPruningRule_HighVolume(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.3
	m := FeedMetrics{
		ProducedLast90d: 300,
		ClicksLast90d:   30,
		ProducedLast30d: 120,
		ClicksLast30d:   25,
		Clicks:          25,
		Exposures:       100,
		CompletedReads:  3, // completion = 0.12 ... wait need <0.05
		ValueScore:      &score,
	}
	// HighVolume needs read_completion = completed/click < 0.05, here 3/25=0.12, not match
	// Adjust: completed=1
	m.CompletedReads = 1
	// 1/25 = 0.04 < 0.05 → match
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R5" {
		t.Errorf("got %+v, want R5", rule)
	}
}

func TestPruningRule_NoMatch(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.5
	m := FeedMetrics{
		ProducedLast90d: 30,
		ClicksLast90d:   15,
		ProducedLast30d: 10,
		ClicksLast30d:   8,
		Clicks:          8,
		Exposures:       20,
		CompletedReads:  5,
		ValueScore:      &score,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule != nil {
		t.Errorf("got %+v, want nil (healthy feed)", rule)
	}
}

func TestPruningRule_PriorityFullyDeadOverDormant(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 0,
		ClicksLast90d:   0,
		ProducedLast30d: 0,
		ClicksLast30d:   0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R1" {
		t.Errorf("priority should give R1 over R3, got %+v", rule)
	}
}

func TestPruningRule_LastFetchedRecent(t *testing.T) {
	// LastFetchedAt is informational only; rules use article counts.
	cfg := config.DefaultFeedHealth()
	now := time.Now()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   3,
		ProducedLast30d: 0,
		ClicksLast30d:   0,
		LastFetchedAt:   &now,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R3" {
		t.Errorf("got %+v, want R3", rule)
	}
}
