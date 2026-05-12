package service

import (
	"net/url"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/rss"
)

// PrerankCandidates returns one [0,1] score per candidate.
//
// The five weights are designed to add up to a bounded score for fast tuning:
//
//	+0.35 topic match in title+editor_note (interest_topics top-10)
//	+0.20 host appears in liked/saved set (last 30 days)
//	+0.15 host appears in completed-read set (last 30 days)
//	-0.25 host appears in disliked set
//	+0.05 baseline (so candidates with no signal still get ordered by doc position)
func PrerankCandidates(cands []rss.Candidate, topics []model.InterestTopic, hosts *model.HostSignalSet) []float64 {
	scores := make([]float64, len(cands))

	top := topics
	if len(top) > 10 {
		top = top[:10]
	}
	needles := make([]string, 0, len(top))
	for _, t := range top {
		n := strings.TrimSpace(strings.ToLower(t.Topic))
		if n != "" {
			needles = append(needles, n)
		}
	}

	for i, c := range cands {
		s := 0.05 // baseline
		hay := strings.ToLower(c.Title + " " + c.EditorNote)
		for _, n := range needles {
			if strings.Contains(hay, n) {
				s += 0.35
				break
			}
		}
		u, err := url.Parse(c.URL)
		if err == nil && u.Host != "" && hosts != nil {
			h := strings.ToLower(u.Hostname())
			if hosts.Liked != nil {
				if _, ok := hosts.Liked[h]; ok {
					s += 0.20
				}
			}
			if hosts.Completed != nil {
				if _, ok := hosts.Completed[h]; ok {
					s += 0.15
				}
			}
			if hosts.Disliked != nil {
				if _, ok := hosts.Disliked[h]; ok {
					s -= 0.25
				}
			}
		}
		if s > 1.0 {
			s = 1.0
		}
		if s < 0 {
			s = 0
		}
		scores[i] = s
	}
	return scores
}
