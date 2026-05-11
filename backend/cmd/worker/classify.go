package main

import (
	"context"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

const (
	classifyBatchSize = 50
	classifyTimeout   = 30 * time.Second
)

var seedTopics = []string{"AI", "金融", "编程", "创业", "科技", "时事", "文化", "健康"}

// runClassifyCycle finds articles with strong signals but no cached topic and
// asks the AI to classify them in one JSON call per article. After classification,
// every user with a strong signal against that article gets the topic + tags
// applied to their interest_topics / interest_tags tables.
func runClassifyCycle(ctx context.Context, articleRepo *repository.ArticleRepository,
	prefRepo *repository.PreferenceRepository, summarizer *ai.Summarizer) {
	if summarizer == nil {
		return
	}

	candidates, err := articleRepo.FindArticlesNeedingClassification(classifyBatchSize)
	if err != nil {
		log.Printf("classify: find candidates: %v", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	vocab := buildVocab(articleRepo)
	log.Printf("classify: %d articles to classify; vocab=%v", len(candidates), vocab[:min(len(vocab), 5)])

	for i := range candidates {
		art := &candidates[i]
		cCtx, cancel := context.WithTimeout(ctx, classifyTimeout)
		cls, err := summarizer.ClassifyArticle(cCtx, art.Title, art.Content, vocab)
		cancel()
		if err != nil {
			log.Printf("classify: article %d failed: %v", art.ID, err)
			continue
		}

		// Always cache (even empty) so we don't retry forever.
		if err := articleRepo.SetClassification(art.ID, cls.Topic, cls.Tags, cls.Category); err != nil {
			log.Printf("classify: SetClassification(%d): %v", art.ID, err)
			continue
		}

		users, err := prefRepo.GetUsersWithStrongSignal(art.ID)
		if err != nil {
			log.Printf("classify: GetUsersWithStrongSignal(%d): %v", art.ID, err)
			continue
		}
		for _, u := range users {
			tw := api.SignalToTopicWeight(u.Strength)
			gw := api.SignalToTagWeight(u.Strength)
			if cls.Topic != "" {
				_ = prefRepo.UpsertTopic(u.UserID, cls.Topic, tw)
			}
			if cls.Category != "" {
				_ = prefRepo.UpsertCategory(u.UserID, cls.Category, tw)
			}
			for _, t := range cls.Tags {
				_ = prefRepo.UpsertTag(u.UserID, t, gw)
			}
		}
		log.Printf("classify: article %d → topic=%q category=%q tags=%v users=%d",
			art.ID, cls.Topic, cls.Category, cls.Tags, len(users))
	}
}

func buildVocab(articleRepo *repository.ArticleRepository) []string {
	top, err := articleRepo.GetTopTopicVocabulary(20)
	if err != nil {
		log.Printf("classify: GetTopTopicVocabulary: %v", err)
		top = nil
	}
	seen := make(map[string]struct{}, len(top)+len(seedTopics))
	out := make([]string, 0, len(top)+len(seedTopics))
	for _, t := range top {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	for _, t := range seedTopics {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}
