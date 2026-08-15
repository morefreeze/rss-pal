package service

import (
	"github.com/bytedance/rss-pal/internal/autotag"
)

type UserTagVocabulary interface {
	GetTagNamesForUser(userID int) ([]string, error)
}

type ArticleTagBinder interface {
	BindByName(articleID, userID int, name string) (int, error)
}

type AutoTagService struct {
	userTags UserTagVocabulary
	binder   ArticleTagBinder
	options  autotag.Options
}

func NewAutoTagService(userTags UserTagVocabulary, binder ArticleTagBinder) *AutoTagService {
	return &AutoTagService{
		userTags: userTags,
		binder:   binder,
		options: autotag.Options{
			SimilarityThreshold: 0.8,
			MaxTags:             3,
		},
	}
}

func (s *AutoTagService) BindGeneratedTags(articleID, userID int, generatedTags, globalVocabulary []string) ([]autotag.Decision, error) {
	userVocabulary, err := s.userTags.GetTagNamesForUser(userID)
	if err != nil {
		return nil, err
	}
	vocabulary := mergeVocabulary(userVocabulary, globalVocabulary)
	decisions := autotag.Resolve(generatedTags, vocabulary, s.options)
	for _, d := range decisions {
		if _, err := s.binder.BindByName(articleID, userID, d.Name); err != nil {
			return nil, err
		}
	}
	return decisions, nil
}

func mergeVocabulary(primary, secondary []string) []string {
	out := make([]string, 0, len(primary)+len(secondary))
	seen := map[string]struct{}{}
	for _, group := range [][]string{primary, secondary} {
		for _, item := range group {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}
