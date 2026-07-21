package api

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

const (
	maxBatchFetchCandidates = 100
	maxBatchURLRunes        = 4096
	maxBatchTitleRunes      = 500
	maxBatchEditorNoteRunes = 2000
)

type BatchFetchCandidate struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	EditorNote string `json:"editor_note"`
}

type BatchFetchRequest struct {
	Candidates []BatchFetchCandidate `json:"candidates"`
}

func validateBatchFetchCandidates(input []BatchFetchCandidate) ([]BatchFetchCandidate, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("no candidates selected")
	}
	if len(input) > maxBatchFetchCandidates {
		return nil, fmt.Errorf("too many candidates: maximum is %d", maxBatchFetchCandidates)
	}

	seen := make(map[string]struct{}, len(input))
	out := make([]BatchFetchCandidate, 0, len(input))
	for i, candidate := range input {
		if utf8.RuneCountInString(candidate.URL) > maxBatchURLRunes {
			return nil, fmt.Errorf("candidate %d URL is too long", i+1)
		}
		normalized, err := rss.NormalizeLinkSetURL(candidate.URL)
		if err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i+1, err)
		}
		title := strings.TrimSpace(candidate.Title)
		note := strings.TrimSpace(candidate.EditorNote)
		if utf8.RuneCountInString(title) > maxBatchTitleRunes {
			return nil, fmt.Errorf("candidate %d title is too long", i+1)
		}
		if utf8.RuneCountInString(note) > maxBatchEditorNoteRunes {
			return nil, fmt.Errorf("candidate %d editor note is too long", i+1)
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		if title == "" {
			title = normalized
		}
		out = append(out, BatchFetchCandidate{Title: title, URL: normalized, EditorNote: note})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no candidates selected")
	}
	return out, nil
}

func linkSetChildInputs(parent *model.Article, candidates []BatchFetchCandidate) []repository.LinkSetChildInput {
	inputs := make([]repository.LinkSetChildInput, 0, len(candidates))
	for _, candidate := range candidates {
		inputs = append(inputs, repository.LinkSetChildInput{
			FeedID:          parent.FeedID,
			ParentArticleID: parent.ID,
			Title:           candidate.Title,
			URL:             candidate.URL,
			EditorNote:      candidate.EditorNote,
			PrerankScore:    0,
			ProcessingState: "processing",
			PublishedAt:     parent.PublishedAt,
		})
	}
	return inputs
}
