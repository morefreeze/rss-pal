package main

import (
	"context"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/repository"
)

// dailyHourCST is 04:00 UTC+8.
const dailyHourCST = 4

const decayFactor = 0.98

// scheduleDailyInsightCron arranges generateDailyInsights to run every 24h at
// 04:00 UTC+8. Stop the returned cancel func to abort. Survives missed wakeups
// (always reschedules from "now → next 04:00").
func scheduleDailyInsightCron(deps insightCronDeps) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			next := nextDaily0400CST(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				log.Printf("daily insight cron: firing at %s", time.Now().Format(time.RFC3339))
				runDailyInsightJob(ctx, deps)
			}
		}
	}()
	return cancel
}

func nextDaily0400CST(now time.Time) time.Time {
	cst := time.FixedZone("CST", 8*3600)
	n := now.In(cst)
	target := time.Date(n.Year(), n.Month(), n.Day(), dailyHourCST, 0, 0, 0, cst)
	if !target.After(n) {
		target = target.Add(24 * time.Hour)
	}
	return target
}

type insightCronDeps struct {
	userRepo         *repository.UserRepository
	prefRepo         *repository.PreferenceRepository
	articleRepo      *repository.ArticleRepository
	userInsightsRepo *repository.UserInsightRepository
	templateRepo     *repository.TemplateRepository
	summarizer       *ai.Summarizer
	defaultModel     string
}

func runDailyInsightJob(ctx context.Context, deps insightCronDeps) {
	if err := deps.prefRepo.DecayAllTopics(decayFactor); err != nil {
		log.Printf("daily cron: DecayAllTopics: %v", err)
	}
	if err := deps.prefRepo.DecayAllTags(decayFactor); err != nil {
		log.Printf("daily cron: DecayAllTags: %v", err)
	}
	// generateDailyInsights filled in by Task 11
	generateDailyInsights(ctx, deps)
}

func generateDailyInsights(ctx context.Context, deps insightCronDeps) {
	// implemented in Task 11
}
