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

// fireBriefings runs the daily job for the last 3 completed days (so a
// transient AI failure on one tick gets retried on the next), and the weekly
// job if `now` is Monday Asia/Shanghai. UserIDsMissing makes per-day work
// idempotent — already-generated users are skipped.
func fireBriefings(ctx context.Context, deps briefingDeps, now time.Time) {
	today := api.TodayLabel(now)
	for k := 1; k <= 3; k++ {
		fireDailyForAllUsers(ctx, deps, today.AddDate(0, 0, -k))
	}
	if isMondayShanghai(now) {
		weekStart := api.MondayLabel(now).AddDate(0, 0, -7)
		fireWeeklyForAllUsers(ctx, deps, weekStart)
	}
}

// dailyBackoffSchedule is the exponential retry pattern after a failed AI
// call: wait 1m, 2m, 4m, 8m, 16m, 32m between attempts (6 total attempts
// ≈ 63 min). After all attempts are spent we write a SQL-Top-5 fallback row
// with no intro so the UI still surfaces articles for that day.
var dailyBackoffSchedule = []time.Duration{
	1 * time.Minute,
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
	32 * time.Minute,
}

// fireDailyForAllUsers picks up the users missing a daily for `day` and runs
// the AI-with-backoff loop per user. Empty candidate pools result in no row.
// On terminal AI failure (all backoffs exhausted), a fallback row of SQL Top 5
// articles with empty intro is written so the day is no longer flagged "pending".
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
			generateDailyWithBackoff(ctx, deps, userID, day, start, end)
		}(uid)
	}
	wg.Wait()
}

// generateDailyWithBackoff calls generateOneDaily, then on AI failure sleeps
// the configured backoff and tries again. On total failure writes a fallback row.
func generateDailyWithBackoff(ctx context.Context, deps briefingDeps, userID int, day, start, end time.Time) {
	for attempt := 0; attempt <= len(dailyBackoffSchedule); attempt++ {
		ok, fatal := generateOneDaily(ctx, deps, userID, day, start, end, attempt+1)
		if ok || fatal {
			return
		}
		if attempt == len(dailyBackoffSchedule) {
			break
		}
		wait := dailyBackoffSchedule[attempt]
		log.Printf("briefing.daily user=%d day=%s: attempt %d failed, retrying in %s", userID, day.Format("2006-01-02"), attempt+1, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
	writeFallbackDaily(ctx, deps, userID, day, start, end)
}

// generateOneDaily runs a single AI attempt. Returns (ok, fatal):
//   - ok=true: row written, done.
//   - fatal=true: terminal condition (no candidates / DB error / nil summarizer) — don't retry.
//   - ok=false, fatal=false: AI parse / call failure — caller may retry.
func generateOneDaily(ctx context.Context, deps briefingDeps, userID int, day, start, end time.Time, attempt int) (ok, fatal bool) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, start, end, 20)
	if err != nil {
		log.Printf("briefing.daily user=%d: GetTopArticlesInRange: %v", userID, err)
		return false, true
	}
	if len(arts) == 0 {
		log.Printf("briefing.daily user=%d day=%s: no candidates, skip", userID, day.Format("2006-01-02"))
		return false, true
	}
	cands := make([]ai.DailyCandidate, len(arts))
	for i, a := range arts {
		cands[i] = ai.DailyCandidate{Idx: i, Title: a.Title, SummaryBrief: a.SummaryBrief}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	sumSem <- struct{}{}
	picks, intro, err := deps.summarizer.GenerateDailyDigest(cctx, cands)
	<-sumSem
	if err != nil {
		log.Printf("briefing.daily user=%d day=%s attempt=%d: ai parse: %v", userID, day.Format("2006-01-02"), attempt, err)
		return false, false
	}
	if len(picks) == 0 {
		return false, true
	}
	ids := make([]int, len(picks))
	for i, p := range picks {
		ids[i] = arts[p].ID
	}
	if err := deps.dailyRepo.Upsert(userID, day, intro, ids); err != nil {
		log.Printf("briefing.daily user=%d day=%s: upsert: %v", userID, day.Format("2006-01-02"), err)
		return false, true
	}
	log.Printf("briefing.daily user=%d day=%s: ok (%d picks, attempt=%d)", userID, day.Format("2006-01-02"), len(ids), attempt)
	return true, false
}

// writeFallbackDaily is invoked once all AI retries are spent. It writes a
// SQL Top 5 selection with an empty intro_text — frontend renders the article
// list and gracefully hides the intro card.
func writeFallbackDaily(ctx context.Context, deps briefingDeps, userID int, day, start, end time.Time) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, start, end, 5)
	if err != nil {
		log.Printf("briefing.daily user=%d day=%s: fallback GetTopArticlesInRange: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	if len(arts) == 0 {
		log.Printf("briefing.daily user=%d day=%s: fallback: no candidates", userID, day.Format("2006-01-02"))
		return
	}
	ids := make([]int, len(arts))
	for i, a := range arts {
		ids[i] = a.ID
	}
	if err := deps.dailyRepo.Upsert(userID, day, "", ids); err != nil {
		log.Printf("briefing.daily user=%d day=%s: fallback upsert: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	log.Printf("briefing.daily user=%d day=%s: fallback written (%d picks, no intro)", userID, day.Format("2006-01-02"), len(ids))
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
