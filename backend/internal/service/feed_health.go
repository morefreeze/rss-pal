package service

import (
	"math"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
)

// FeedMetrics is the per-feed aggregation result computed by the repo layer.
// Window-prefixed counts (Last30d, Last90d) carry the wider 90d data needed by
// pruning rules even when the user is viewing the 30d dashboard.
type FeedMetrics struct {
	FeedID    int
	FeedTitle string

	// Window-bound (matches the user-selected window 30d/90d for display)
	Produced        int
	Exposures       int
	Clicks          int
	CompletedReads  int
	AvgDurationMin  float64
	FeedbackDensity float64
	LastActiveAt    *time.Time

	// Always 30d (for pruning rules)
	ProducedLast30d int
	ClicksLast30d   int
	// Always 90d (for pruning rules)
	ProducedLast90d int
	ClicksLast90d   int

	LastFetchedAt *time.Time

	// ValueScore is nil for cold start (Exposures < ColdStartMinExposures).
	ValueScore *float64
}

// PruningRule is a hint about an unhealthy feed.
type PruningRule struct {
	ID               string   `json:"id"`               // R1..R5
	Label            string   `json:"label"`            // 中文人类标签
	Reason           string   `json:"reason"`           // 一句解释，给抽屉直接展示
	SuggestedActions []string `json:"suggested_actions"` // ["归档","暂停","降权"] 中的子集
}

// ComputeValueScore returns NaN for cold-start metrics, else the weighted score.
// Formula: 0.35*ctr + 0.35*completion + 0.20*norm(avg_duration,10min) + 0.10*norm(feedback_density,5)
func ComputeValueScore(m FeedMetrics) float64 {
	if m.Exposures < config.DefaultFeedHealth().ColdStartMinExposures {
		return math.NaN()
	}
	ctr := safeDiv(float64(m.Clicks), float64(m.Exposures))
	completion := safeDiv(float64(m.CompletedReads), float64(m.Clicks))
	normDur := normalize(m.AvgDurationMin, 10.0)
	normFb := normalize(m.FeedbackDensity, 5.0)
	return 0.35*ctr + 0.35*completion + 0.20*normDur + 0.10*normFb
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// normalize clamps x/target to [0, 1]. Negative inputs (possible when
// dislike > like+save) are clamped to 0.
func normalize(x, target float64) float64 {
	if target == 0 {
		return 0
	}
	v := x / target
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// EvaluatePruningRule applies R1-R5 in priority order. Returns nil if healthy.
func EvaluatePruningRule(m FeedMetrics, cfg config.FeedHealthConfig) *PruningRule {
	// R1 完全失效 — highest priority
	if m.ProducedLast90d == 0 && m.ClicksLast90d == 0 {
		return &PruningRule{
			ID:               "R1",
			Label:            "完全失效",
			Reason:           "90 天内无新文章且 0 次点击",
			SuggestedActions: []string{"归档"},
		}
	}
	// R2 沉睡型
	if m.ClicksLast30d == 0 && m.ProducedLast30d >= cfg.DormantMinArticles {
		return &PruningRule{
			ID:               "R2",
			Label:            "沉睡型",
			Reason:           "30 天内你 0 次点击该 feed（30 天产出 ≥ 3）",
			SuggestedActions: []string{"归档"},
		}
	}
	// R3 死源型
	if m.ProducedLast30d == 0 {
		return &PruningRule{
			ID:               "R3",
			Label:            "死源型",
			Reason:           "30 天内 feed 抓回 0 篇文章",
			SuggestedActions: []string{"暂停", "归档"},
		}
	}
	// R4 低价值
	if m.ValueScore != nil && *m.ValueScore < cfg.LowValueScoreThreshold && m.ProducedLast30d >= cfg.LowValueMinSampleSize {
		return &PruningRule{
			ID:               "R4",
			Label:            "低价值",
			Reason:           "价值得分低于阈值且样本充足",
			SuggestedActions: []string{"归档", "降权"},
		}
	}
	// R5 过水型
	if m.ProducedLast30d > cfg.HighVolumeArticleCount && safeDiv(float64(m.CompletedReads), float64(m.Clicks)) < cfg.HighVolumeMaxCompletionRate {
		return &PruningRule{
			ID:               "R5",
			Label:            "过水型",
			Reason:           "30 天文章 > 100 篇，但完读率 < 5%",
			SuggestedActions: []string{"降权"},
		}
	}
	return nil
}
