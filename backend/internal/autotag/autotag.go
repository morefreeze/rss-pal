package autotag

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Action string

const (
	ReuseExisting Action = "reuse_existing"
	CreateNew     Action = "create_new"
)

type Options struct {
	SimilarityThreshold float64
	MaxTags             int
}

type Decision struct {
	Name       string
	Action     Action
	Similarity float64
	Source     string
}

type candidateTag struct {
	display string
	key     string
	vector  map[string]float64
}

var genericTags = map[string]struct{}{
	"ai": {}, "人工智能": {}, "文章": {}, "内容": {}, "技术": {}, "科技": {},
	"新闻": {}, "资讯": {}, "动态": {}, "博客": {}, "随笔": {}, "观点": {},
}

var genericSuffixes = []string{"公司", "文章", "新闻", "资讯", "动态", "技术", "内容", "主题"}

func Resolve(candidates []string, existing []string, opts Options) []Decision {
	if opts.SimilarityThreshold <= 0 {
		opts.SimilarityThreshold = 0.8
	}
	if opts.MaxTags <= 0 {
		opts.MaxTags = 3
	}

	existingTags := make([]candidateTag, 0, len(existing))
	for _, name := range existing {
		if t, ok := clean(name); ok {
			existingTags = append(existingTags, t)
		}
	}

	out := make([]Decision, 0, opts.MaxTags)
	used := map[string]struct{}{}
	for _, raw := range candidates {
		tag, ok := clean(raw)
		if !ok {
			continue
		}

		best, score := bestExisting(tag, existingTags)
		if score >= opts.SimilarityThreshold {
			if _, ok := used[best.key]; ok {
				continue
			}
			used[best.key] = struct{}{}
			out = append(out, Decision{
				Name:       best.display,
				Action:     ReuseExisting,
				Similarity: score,
				Source:     tag.display,
			})
		} else {
			if _, ok := used[tag.key]; ok {
				continue
			}
			used[tag.key] = struct{}{}
			out = append(out, Decision{
				Name:       tag.display,
				Action:     CreateNew,
				Similarity: score,
				Source:     tag.display,
			})
		}

		if len(out) >= opts.MaxTags {
			break
		}
	}
	return out
}

func bestExisting(tag candidateTag, existing []candidateTag) (candidateTag, float64) {
	var best candidateTag
	bestScore := 0.0
	for _, item := range existing {
		score := similarity(tag, item)
		if score > bestScore {
			best = item
			bestScore = score
		}
	}
	return best, bestScore
}

func similarity(a, b candidateTag) float64 {
	if a.key == "" || b.key == "" {
		return 0
	}
	if a.key == b.key {
		return 1
	}
	if strings.Contains(a.key, b.key) || strings.Contains(b.key, a.key) {
		shorter := len([]rune(a.key))
		if n := len([]rune(b.key)); n < shorter {
			shorter = n
		}
		if shorter >= 3 {
			return 0.9
		}
	}
	return cosine(a.vector, b.vector)
}

func clean(raw string) (candidateTag, bool) {
	display := strings.TrimSpace(raw)
	display = strings.Join(strings.Fields(display), " ")
	if display == "" || utf8.RuneCountInString(display) > 32 {
		return candidateTag{}, false
	}

	key := normalize(display)
	for _, suffix := range genericSuffixes {
		key = strings.TrimSuffix(key, normalize(suffix))
	}
	if key == "" || utf8.RuneCountInString(key) < 2 {
		return candidateTag{}, false
	}
	if _, ok := genericTags[key]; ok {
		return candidateTag{}, false
	}
	return candidateTag{display: display, key: key, vector: vectorize(key)}, true
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '・' || r == '·' {
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	key := b.String()
	replacer := strings.NewReplacer(
		"db", "数据库",
		"llm", "大模型",
	)
	return replacer.Replace(key)
}

func vectorize(key string) map[string]float64 {
	runes := []rune(key)
	v := map[string]float64{}
	if len(runes) == 0 {
		return v
	}
	for _, r := range runes {
		v[string(r)] += 0.5
	}
	if len(runes) == 1 {
		v[string(runes)] += 1
		return v
	}
	for i := 0; i < len(runes)-1; i++ {
		v[string(runes[i:i+2])] += 1
	}
	if len(runes) <= 4 {
		v[string(runes)] += 1
	}
	return v
}

func cosine(a, b map[string]float64) float64 {
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	dot := 0.0
	a2 := 0.0
	for _, k := range keys {
		av := a[k]
		dot += av * b[k]
		a2 += av * av
	}
	b2 := 0.0
	for _, bv := range b {
		b2 += bv * bv
	}
	if a2 == 0 || b2 == 0 {
		return 0
	}
	return dot / (math.Sqrt(a2) * math.Sqrt(b2))
}
