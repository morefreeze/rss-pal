package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

const briefingHourCST = 5

var briefingShanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// nextBriefingFire returns the next 05:00 Asia/Shanghai strictly after `now`.
func nextBriefingFire(now time.Time) time.Time {
	n := now.In(briefingShanghai)
	target := time.Date(n.Year(), n.Month(), n.Day(), briefingHourCST, 0, 0, 0, briefingShanghai)
	if !target.After(n) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func isMondayShanghai(t time.Time) bool {
	return t.In(briefingShanghai).Weekday() == time.Monday
}

type briefingDeps struct {
	articleRepo *repository.ArticleRepository
	dailyRepo   *repository.DailyDigestRepository
	weeklyRepo  *repository.WeeklyDigestRepository
	summarizer  *ai.Summarizer
}

// scheduleBriefingCron fires fireDailyForAllUsers every day at 05:00 Asia/Shanghai,
// and fireWeeklyForAllUsers additionally on Mondays. Mirrors scheduleDailyInsightCron.
// Dev hook: BRIEFING_RUN_NOW=1 fires both jobs immediately on startup.
func scheduleBriefingCron(deps briefingDeps) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		runBriefingCatchUp(ctx, deps)
		if os.Getenv("BRIEFING_RUN_NOW") == "1" {
			log.Printf("briefing cron: BRIEFING_RUN_NOW=1 → firing immediately")
			fireBriefings(ctx, deps, time.Now())
		}
		for {
			next := nextBriefingFire(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				log.Printf("briefing cron: firing at %s", time.Now().Format(time.RFC3339))
				fireBriefings(ctx, deps, time.Now())
			}
		}
	}()
	return cancel
}

// fireBriefings runs the daily job, and if `now` is Monday Asia/Shanghai, the weekly job too.
func fireBriefings(ctx context.Context, deps briefingDeps, now time.Time) {
	today := api.TodayLabel(now)
	yesterday := today.AddDate(0, 0, -1)
	fireDailyForAllUsers(ctx, deps, yesterday)
	if isMondayShanghai(now) {
		weekStart := api.MondayLabel(now).AddDate(0, 0, -7)
		fireWeeklyForAllUsers(ctx, deps, weekStart)
	}
}

// fireDailyForAllUsers picks up the users missing a daily for `day` and generates one each.
// AI errors / empty candidate pools result in no row written.
func fireDailyForAllUsers(ctx context.Context, deps briefingDeps, day time.Time) {
	if deps.summarizer == nil {
		log.Printf("briefing: daily skipped, no summarizer (CLAUDE_API_KEY?)")
		return
	}
	ids, err := deps.dailyRepo.UserIDsMissing(day)
	if err != nil {
		log.Printf("briefing.daily: UserIDsMissing(%s): %v", day.Format("2006-01-02"), err)
		return
	}
	if len(ids) == 0 {
		return
	}
	log.Printf("briefing.daily: %d users to generate for %s", len(ids), day.Format("2006-01-02"))
	start, end := api.DailyWindow(day)
	var wg sync.WaitGroup
	for _, uid := range ids {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			generateOneDaily(ctx, deps, userID, day, start, end)
		}(uid)
	}
	wg.Wait()
}

func generateOneDaily(ctx context.Context, deps briefingDeps, userID int, day, start, end time.Time) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, start, end, 20)
	if err != nil {
		log.Printf("briefing.daily user=%d: GetTopArticlesInRange: %v", userID, err)
		return
	}
	if len(arts) == 0 {
		log.Printf("briefing.daily user=%d day=%s: no candidates, skip", userID, day.Format("2006-01-02"))
		return
	}
	cands := make([]ai.DailyCandidate, len(arts))
	for i, a := range arts {
		cands[i] = ai.DailyCandidate{Idx: i, Title: a.Title, SummaryBrief: a.SummaryBrief}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	picks, intro, err := deps.summarizer.GenerateDailyDigest(cctx, cands)
	if err != nil {
		log.Printf("briefing.daily user=%d day=%s: ai parse: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	if len(picks) == 0 {
		return
	}
	ids := make([]int, len(picks))
	for i, p := range picks {
		ids[i] = arts[p].ID
	}
	if err := deps.dailyRepo.Upsert(userID, day, intro, ids); err != nil {
		log.Printf("briefing.daily user=%d day=%s: upsert: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	log.Printf("briefing.daily user=%d day=%s: ok (%d picks)", userID, day.Format("2006-01-02"), len(ids))
}

// fireWeeklyForAllUsers picks up users missing a weekly for `weekStart` and generates one each.
func fireWeeklyForAllUsers(ctx context.Context, deps briefingDeps, weekStart time.Time) {
	if deps.summarizer == nil {
		log.Printf("briefing: weekly skipped, no summarizer")
		return
	}
	ids, err := deps.weeklyRepo.UserIDsMissing(weekStart)
	if err != nil {
		log.Printf("briefing.weekly: UserIDsMissing(%s): %v", weekStart.Format("2006-01-02"), err)
		return
	}
	if len(ids) == 0 {
		return
	}
	log.Printf("briefing.weekly: %d users to generate for week of %s", len(ids), weekStart.Format("2006-01-02"))
	end := weekStart.AddDate(0, 0, 7)
	var wg sync.WaitGroup
	for _, uid := range ids {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			generateOneWeekly(ctx, deps, userID, weekStart, end)
		}(uid)
	}
	wg.Wait()
}

func generateOneWeekly(ctx context.Context, deps briefingDeps, userID int, weekStart, end time.Time) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, weekStart, end, 10)
	if err != nil {
		log.Printf("briefing.weekly user=%d: GetTopArticlesInRange: %v", userID, err)
		return
	}
	if len(arts) == 0 {
		log.Printf("briefing.weekly user=%d week=%s: no candidates, skip", userID, weekStart.Format("2006-01-02"))
		return
	}
	items := make([]ai.WeeklyDigestItem, len(arts))
	for i, a := range arts {
		items[i] = ai.WeeklyDigestItem{Title: a.Title, SummaryBrief: a.SummaryBrief}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	intro, err := deps.summarizer.GenerateWeeklyIntro(cctx, items)
	if err != nil || intro == "" {
		if err != nil {
			log.Printf("briefing.weekly user=%d week=%s: ai: %v", userID, weekStart.Format("2006-01-02"), err)
		}
		return
	}
	ids := make([]int, len(arts))
	for i, a := range arts {
		ids[i] = a.ID
	}
	if err := deps.weeklyRepo.Upsert(userID, weekStart, intro, ids); err != nil {
		log.Printf("briefing.weekly user=%d week=%s: upsert: %v", userID, weekStart.Format("2006-01-02"), err)
		return
	}
	log.Printf("briefing.weekly user=%d week=%s: ok", userID, weekStart.Format("2006-01-02"))
}

// runBriefingCatchUp generates any missing dailies for the last 3 completed days
// and the last completed weekly. Called once at worker startup.
func runBriefingCatchUp(ctx context.Context, deps briefingDeps) {
	now := time.Now()
	today := api.TodayLabel(now)
	for k := 1; k <= 3; k++ {
		fireDailyForAllUsers(ctx, deps, today.AddDate(0, 0, -k))
	}
	fireWeeklyForAllUsers(ctx, deps, api.MondayLabel(now).AddDate(0, 0, -7))
}
