package service

import (
	"context"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
)

type SummarizerService struct {
	summarizer *ai.Summarizer
}

func NewSummarizerService(summarizer *ai.Summarizer) *SummarizerService {
	return &SummarizerService{summarizer: summarizer}
}

func (s *SummarizerService) Summarize(ctx context.Context, article *model.Article) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}

	result, err := s.summarizer.Summarize(ctx, article.Title, content)
	if err != nil {
		return "", "", err
	}

	return result.Brief, result.Detailed, nil
}

func (s *SummarizerService) SummarizeWithTemplate(ctx context.Context, article *model.Article, briefPrompt, detailedPrompt string) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}

	result, err := s.summarizer.SummarizeWithTemplate(ctx, article.Title, content, briefPrompt, detailedPrompt)
	if err != nil {
		return "", "", err
	}

	return result.Brief, result.Detailed, nil
}

func (s *SummarizerService) SummarizeStream(ctx context.Context, article *model.Article,
	onBriefDelta, onDetailedDelta func(string)) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeStream(ctx, article.Title, content, onBriefDelta, onDetailedDelta)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}

func (s *SummarizerService) SummarizeWithTemplateStream(ctx context.Context, article *model.Article,
	briefPrompt, detailedPrompt string,
	onBriefDelta, onDetailedDelta func(string)) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeWithTemplateStream(ctx, article.Title, content, briefPrompt, detailedPrompt, onBriefDelta, onDetailedDelta)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}

func (s *SummarizerService) ExtractTopics(ctx context.Context, article *model.Article) ([]string, error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}

	return s.summarizer.ExtractTopics(ctx, article.Title, content)
}
