package rss

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// LinkSetSuggestionMinCandidates and LinkSetSuggestionMaxGapSegments are the
// thresholds for the "potential link_set" detector that runs against regular
// rss articles. The rule:
//
//   - At least N candidate links live as siblings under a common parent,
//     each occupying its own line container (li / p / dt / dd / tr).
//   - At most G gap segments (contiguous runs of non-candidate siblings)
//     appear between candidates in the run. Trailing gaps don't count.
//
// These knobs are deliberately strict so the suggestion only fires for
// obvious "list of links" patterns (newsletters, awesome lists, link rolls).
const (
	LinkSetSuggestionMinCandidates  = 11
	LinkSetSuggestionMaxGapSegments = 2
)

// linkSetLineTags are the HTML tags whose elements act as "line containers"
// in the suggestion detector — a candidate link wrapped in one of these
// counts as occupying one line.
var linkSetLineTags = map[string]struct{}{
	"li": {}, "p": {}, "dt": {}, "dd": {}, "tr": {},
}

// DetectLinkSetSuggestion runs the standard candidate filter (same as
// ExtractCandidates) and then looks for a continuous one-per-line block of
// candidate links among same-parent same-tag siblings. Returns the
// candidates inside the qualifying run (in DOM order) along with true when
// the run has >= LinkSetSuggestionMinCandidates lines and <=
// LinkSetSuggestionMaxGapSegments gap segments. Otherwise returns (nil, false).
func DetectLinkSetSuggestion(htmlContent, parentURL string) ([]Candidate, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, false
	}

	type located struct {
		cand     Candidate
		lineNode *html.Node
	}
	var all []located
	walkCandidates(doc, parentURL, func(c Candidate, sel *goquery.Selection) {
		var ln *html.Node
		for n := sel.Get(0); n != nil; n = n.Parent {
			if n.Type != html.ElementNode {
				continue
			}
			if _, ok := linkSetLineTags[n.Data]; ok {
				ln = n
				break
			}
		}
		all = append(all, located{cand: c, lineNode: ln})
	})

	if len(all) < LinkSetSuggestionMinCandidates {
		return nil, false
	}

	type groupKey struct {
		parent *html.Node
		tag    string
	}
	groups := map[groupKey]map[*html.Node]Candidate{}
	for _, p := range all {
		if p.lineNode == nil || p.lineNode.Parent == nil {
			continue
		}
		k := groupKey{parent: p.lineNode.Parent, tag: p.lineNode.Data}
		if _, ok := groups[k]; !ok {
			groups[k] = map[*html.Node]Candidate{}
		}
		// If two candidates share a line node (e.g. <li> with two links),
		// keep the first — the suggestion cares about line-count, not
		// link-count, and the line is still "occupied".
		if _, dup := groups[k][p.lineNode]; !dup {
			groups[k][p.lineNode] = p.cand
		}
	}

	var best []Candidate
	for k, nodeToCand := range groups {
		if len(nodeToCand) < LinkSetSuggestionMinCandidates {
			continue
		}
		var siblings []*html.Node
		for c := k.parent.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == k.tag {
				siblings = append(siblings, c)
			}
		}
		run := findLongestQualifyingRun(siblings, nodeToCand)
		if len(run) > len(best) {
			best = run
		}
	}

	if len(best) < LinkSetSuggestionMinCandidates {
		return nil, false
	}
	return best, true
}

// findLongestQualifyingRun scans siblings (same-tag children of one parent,
// in DOM order) and returns the longest contiguous candidate run that
// satisfies the gap-segment cap. Returns nil if no run reaches the minimum.
func findLongestQualifyingRun(siblings []*html.Node, nodeToCand map[*html.Node]Candidate) []Candidate {
	var best []Candidate
	for i := 0; i < len(siblings); i++ {
		startCand, ok := nodeToCand[siblings[i]]
		if !ok {
			continue
		}
		run := []Candidate{startCand}
		gapSegs := 0
		inGap := false
		for j := i + 1; j < len(siblings); j++ {
			if c, ok := nodeToCand[siblings[j]]; ok {
				if inGap {
					gapSegs++
					if gapSegs > LinkSetSuggestionMaxGapSegments {
						break
					}
					inGap = false
				}
				run = append(run, c)
			} else {
				inGap = true
			}
		}
		if len(run) >= LinkSetSuggestionMinCandidates && len(run) > len(best) {
			best = run
		}
	}
	return best
}
