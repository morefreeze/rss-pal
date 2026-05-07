package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
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
	// generateDailyInsights filled in by Task 11
	generateDailyInsights(ctx, deps)
}

const insightTokenBudget = 6000 // approx; chinese chars ~1.5 tokens each

// estimateTokens is intentionally approximate: 1 token ≈ 2 bytes for Chinese-heavy text.
func estimateTokens(s string) int {
	return len(s) / 2
}

type pickedArticle struct {
	Article model.Article
	Brief   string
	Detail  string
	Signal  string // "save"|"like"|"read"|"dislike"
	Within7 bool
}

// buildLayeredPrompt builds the per-user insight prompt. Currently the cron
// passes empty L1/L2/L3 (only topics+tags drive the analysis); the helper
// accepts them for future extension. See spec §4.4.
func buildLayeredPrompt(topics []model.InterestTopic, tags []model.InterestTag,
	l3, l2, l1 []pickedArticle) string {
	var b strings.Builder
	b.WriteString("基于用户的兴趣画像与多层级阅读行为，请进行个性化洞察分析。\n\n")

	if len(topics) > 0 {
		b.WriteString("## 用户兴趣主题（粗粒度，按权重，已做时间衰减）\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "- %s (%.2f)\n", t.Topic, t.Weight)
		}
		b.WriteString("\n")
	}

	if len(tags) > 0 {
		b.WriteString("## 用户关键词（细粒度，top 20，按权重）\n")
		max := 20
		if len(tags) < max {
			max = len(tags)
		}
		for i := 0; i < max; i++ {
			fmt.Fprintf(&b, "- %s (%.2f)\n", tags[i].Tag, tags[i].Weight)
		}
		b.WriteString("\n")
	}

	if len(l3) > 0 {
		b.WriteString("## 高强度信号（深度互动，含详细总结）\n")
		for i, p := range l3 {
			fmt.Fprintf(&b, "%d. [%s] 标题：%s\n   摘要：%s\n",
				i+1, p.Signal, p.Article.Title, nonEmpty(p.Detail, p.Brief))
		}
		b.WriteString("\n")
	}

	if len(l2) > 0 {
		b.WriteString("## 强信号（含 brief）\n")
		for _, p := range l2 {
			fmt.Fprintf(&b, "- [%s] %s\n  要点：%s\n", p.Signal, p.Article.Title, p.Brief)
		}
		b.WriteString("\n")
	}

	if len(l1) > 0 {
		b.WriteString("## 浏览过的文章（仅标题）\n")
		var w7, w30 []string
		for _, p := range l1 {
			if p.Within7 {
				w7 = append(w7, p.Article.Title)
			} else {
				w30 = append(w30, p.Article.Title)
			}
		}
		if len(w7) > 0 {
			fmt.Fprintf(&b, "- 近 7 天：%s\n", strings.Join(w7, "、"))
		}
		if len(w30) > 0 {
			fmt.Fprintf(&b, "- 近 30 天：%s\n", strings.Join(w30, "、"))
		}
		b.WriteString("\n")
	}

	b.WriteString("请用中文 markdown 输出：\n" +
		"1. **核心兴趣领域**（3-5 个，按确定性排序，结合主题与高频标签）\n" +
		"2. **近期偏好变化**（对比 7d vs 30d）\n" +
		"3. **可能的新兴趣点**（弱信号但反复出现）\n" +
		"4. **阅读建议**（结合\"不感兴趣\"做反向建议）\n")
	return b.String()
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
		prompt := buildLayeredPrompt(topics, tags, nil, nil, nil)
		if estimateTokens(prompt) > insightTokenBudget {
			log.Printf("daily cron: user %d prompt too long, skipping", u.ID)
			continue
		}
		content, err := deps.summarizer.GenerateUserInsight(ctx, prompt)
		if err != nil {
			log.Printf("daily cron: user %d generate: %v", u.ID, err)
			continue
		}
		if err := deps.userInsightsRepo.Insert(u.ID, content, "auto", deps.defaultModel); err != nil {
			log.Printf("daily cron: user %d insert: %v", u.ID, err)
			continue
		}
		log.Printf("daily cron: user %d ok (topics=%d tags=%d, %dB)", u.ID, len(topics), len(tags), len(content))
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
