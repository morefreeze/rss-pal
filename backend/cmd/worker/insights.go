package main

import (
	"context"
	"log"
	"os"
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
//
// Dev hook: setting INSIGHTS_RUN_NOW=1 fires runDailyInsightJob once on startup
// before entering the regular schedule loop.
func scheduleDailyInsightCron(deps insightCronDeps) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if os.Getenv("INSIGHTS_RUN_NOW") == "1" {
			log.Printf("daily insight cron: INSIGHTS_RUN_NOW=1 → firing immediately")
			runDailyInsightJob(ctx, deps)
		}
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
	generateDailyInsights(ctx, deps)
}

func generateDailyInsights(ctx context.Context, deps insightCronDeps) {
	users, err := deps.userRepo.ListAll()
	if err != nil {
		log.Printf("daily cron: ListAll users: %v", err)
		return
	}
	for _, u := range users {
		topics, _ := deps.prefRepo.GetTopics(u.ID)
		tags, _ := deps.prefRepo.GetTags(u.ID)
		if len(topics) == 0 && len(tags) == 0 {
			continue
		}
		titles, _ := deps.prefRepo.GetRecentReadTitles(u.ID, 20)
		candidates, err := deps.articleRepo.GetInsightCandidates(u.ID, 40, 10)
		if err != nil {
			log.Printf("daily cron: user %d GetInsightCandidates: %v", u.ID, err)
			candidates = nil
		}

		prompt := ai.BuildInsightPrompt(topics, tags, titles, candidates)
		id, err := deps.userInsightsRepo.InsertPending(u.ID, "auto", deps.defaultModel)
		if err != nil {
			log.Printf("daily cron: user %d InsertPending: %v", u.ID, err)
			continue
		}
		raw, err := deps.summarizer.GenerateUserInsightJSON(ctx, prompt)
		if err != nil {
			log.Printf("daily cron: user %d generate: %v", u.ID, err)
			_ = deps.userInsightsRepo.MarkFailed(id, err.Error())
			continue
		}
		idSet := make(map[int]bool, len(candidates))
		for _, c := range candidates {
			idSet[c.Article.ID] = true
		}
		markdown, recs, dropped := ai.ParseInsightJSON(raw, idSet)
		if len(dropped) > 0 {
			log.Printf("daily cron: user %d dropped %d entries: %v", u.ID, len(dropped), dropped)
		}
		if err := deps.userInsightsRepo.MarkDoneWithRecs(id, markdown, recs); err != nil {
			log.Printf("daily cron: user %d MarkDoneWithRecs: %v", u.ID, err)
			continue
		}
		log.Printf("daily cron: user %d ok (topics=%d tags=%d, %dB md, %d recs)",
			u.ID, len(topics), len(tags), len(markdown), len(recs))
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
