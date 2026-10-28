package ai

import (
	"context"
	"github.com/bytedance/rss-pal/internal/airouting"
	"unicode/utf8"
)

type ArticleResolver func(context.Context, int) (airouting.Target, error)

// Article input has a separate bound from stored content and digest snippets.
// Browser and server summaries must see the same long-article context.
const maxArticleContentRunes = 100000

// Only platform clients opt in. Personal API clients never inherit this resolver.
func (s *Summarizer) SetArticleResolver(resolve ArticleResolver) { s.articleResolver = resolve }
func (s *Summarizer) DisableArticleRouting()                     { s.articleModelSelected = true }

// Select before truncation. A private copy pins provider, credentials and model
// across brief/detailed and vision fallbacks without mutating the shared client.
func (s *Summarizer) forArticle(ctx context.Context, content string) *Summarizer {
	if s.articleModelSelected {
		return s
	}
	if s.articleResolver != nil {
		selected := *s
		selected.articleModelSelected = true
		selected.articleRouted = true
		target, err := s.articleResolver(ctx, utf8.RuneCountInString(content))
		selected.routingErr = err
		if err == nil {
			selected.model = target.Model
			selected.baseURL = target.BaseURL
			selected.apiKey = target.APIKey
			selected.providerProtocol = target.Protocol
			// Preserve the existing vision model only with its original credentials
			// and endpoint. Never send it to another company.
			if target.BaseURL != s.baseURL || target.APIKey != s.apiKey || s.visionModel == "" {
				selected.visionModel = target.Model
			}
		}
		return &selected
	}
	if s.model != "glm-5.3" && s.model != "glm-5.3-flash" {
		return s
	}
	selected := *s
	selected.articleModelSelected = true
	selected.model = "glm-5.3-flash"
	if utf8.RuneCountInString(content) > 10000 {
		selected.model = "glm-5.3"
	}
	return &selected
}
func (s *Summarizer) truncateArticle(content string) string {
	return prepareArticleContent(content)
}

func prepareArticleContent(content string) string {
	r := []rune(content)
	if len(r) > maxArticleContentRunes {
		return string(r[:maxArticleContentRunes]) + "\n...(内容已截断)"
	}
	return content
}
