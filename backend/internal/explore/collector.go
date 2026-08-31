package explore

import (
	"container/heap"
	"sort"
)

// candidateCollector retains the lexically smallest keys while coalescing
// repeated keys. Its map never exceeds maxK.
type candidateCollector struct {
	maxK  int
	key   func(Candidate) string
	items map[string]Candidate
	keys  candidateKeyHeap
}

func newCandidateCollector(maxK int, key func(Candidate) string) *candidateCollector {
	collector := &candidateCollector{maxK: maxK, key: key, items: make(map[string]Candidate, maxK)}
	heap.Init(&collector.keys)
	return collector
}

func (collector *candidateCollector) add(candidate Candidate) {
	if collector.maxK <= 0 {
		return
	}
	key := collector.key(candidate)
	if current, exists := collector.items[key]; exists {
		collector.items[key] = mergeCandidates(current, candidate)
		return
	}
	if len(collector.items) == collector.maxK {
		largest := collector.keys[0]
		if key >= largest {
			return
		}
		delete(collector.items, largest)
		heap.Pop(&collector.keys)
	}
	collector.items[key] = candidateWithNormalizedTags(candidate)
	heap.Push(&collector.keys, key)
}

func (collector *candidateCollector) candidates() []Candidate {
	result := make([]Candidate, 0, len(collector.items))
	for _, candidate := range collector.items {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		leftKey, rightKey := collector.key(result[i]), collector.key(result[j])
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		return candidateTupleLess(result[i], result[j])
	})
	return result
}

func mergeCandidates(left, right Candidate) Candidate {
	occurrences := safeOccurrenceAdd(left.OccurrenceCount, right.OccurrenceCount)
	tags := uniqueStrings(append(left.Tags, right.Tags...))
	if candidateTupleLess(right, left) {
		left = right
	}
	left.OccurrenceCount = occurrences
	left.Tags = tags
	return left
}

func candidateWithNormalizedTags(candidate Candidate) Candidate {
	candidate.Tags = uniqueStrings(candidate.Tags)
	return candidate
}

func candidateTupleLess(left, right Candidate) bool {
	for _, pair := range [][2]string{
		{left.FeedURL, right.FeedURL},
		{left.ExternalKey, right.ExternalKey},
		{left.Title, right.Title},
		{left.SiteURL, right.SiteURL},
		{left.Topic, right.Topic},
	} {
		if pair[0] != pair[1] {
			return pair[0] < pair[1]
		}
	}
	return false
}

// candidateKeyHeap is a max-heap, so its root is the lexically largest key.
type candidateKeyHeap []string

func (heap candidateKeyHeap) Len() int           { return len(heap) }
func (heap candidateKeyHeap) Less(i, j int) bool { return heap[i] > heap[j] }
func (heap candidateKeyHeap) Swap(i, j int)      { heap[i], heap[j] = heap[j], heap[i] }
func (heap *candidateKeyHeap) Push(value any)    { *heap = append(*heap, value.(string)) }
func (heap *candidateKeyHeap) Pop() any {
	old := *heap
	last := len(old) - 1
	value := old[last]
	*heap = old[:last]
	return value
}
