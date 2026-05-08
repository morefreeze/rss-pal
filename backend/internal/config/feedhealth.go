package config

import "time"

// FeedHealthConfig holds thresholds for feed health metrics and pruning rules.
// Self-use, hardcoded — change values and restart to tune.
type FeedHealthConfig struct {
	DormantClickWindow          time.Duration // R2 沉睡型: 该窗口内 0 点击
	DormantMinArticles          int           // R2: 至少要有这么多文章才考虑沉睡判定
	DeadFeedArticleWindow       time.Duration // R3 死源型: 该窗口内 0 文章
	FullyDeadWindow             time.Duration // R1 完全失效: 该窗口同时 0 文章 0 点击
	LowValueScoreThreshold      float64       // R4 低价值: value_score 低于此值
	LowValueMinSampleSize       int           // R4: 至少这么多文章才参与低价值判定
	HighVolumeArticleCount      int           // R5 过水型: 30d 文章数 >此值
	HighVolumeMaxCompletionRate float64       // R5: 完读率低于此值
	ColdStartMinExposures       int           // 曝光数 < 此值时 value_score 为 null
}

func DefaultFeedHealth() FeedHealthConfig {
	return FeedHealthConfig{
		DormantClickWindow:          30 * 24 * time.Hour,
		DormantMinArticles:          3,
		DeadFeedArticleWindow:       30 * 24 * time.Hour,
		FullyDeadWindow:             90 * 24 * time.Hour,
		LowValueScoreThreshold:      0.1,
		LowValueMinSampleSize:       10,
		HighVolumeArticleCount:      100,
		HighVolumeMaxCompletionRate: 0.05,
		ColdStartMinExposures:       10,
	}
}
