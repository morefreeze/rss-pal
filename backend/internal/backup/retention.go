package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// RetentionPolicy controls how aggressively backups are pruned by age.
// Defaults: keep everything in the last 7d, weekly between 7-30d, monthly
// beyond 30d.
type RetentionPolicy struct {
	KeepAllWithin    time.Duration // age < this: keep all
	WeeklyUntil      time.Duration // age < this (and >= KeepAllWithin): keep 1/week
	// >= WeeklyUntil: keep 1/month
}

// DefaultRetention is the policy applied by Prune unless overridden.
var DefaultRetention = RetentionPolicy{
	KeepAllWithin: 7 * 24 * time.Hour,
	WeeklyUntil:   30 * 24 * time.Hour,
}

// Prune scans dir, applies the retention policy with `now` as the reference
// point, and deletes files that don't survive. Returns the list of deleted
// filenames. dir is unchanged if it doesn't exist.
//
// Bucketing: within each non-"keep-all" bucket (one ISO week or one month),
// only the most recent backup is kept; older entries in the same bucket are
// removed. The most-recent backup overall is always preserved, regardless of
// age, so a long-idle deployment never loses its last known state.
func Prune(dir string, now time.Time, policy RetentionPolicy) ([]string, error) {
	files, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Sort newest-first so the first entry in each bucket is the one we keep.
	sort.Slice(files, func(i, j int) bool { return files[i].CreatedAt.After(files[j].CreatedAt) })

	keep := map[string]bool{files[0].Name: true} // always keep newest
	seenBucket := map[string]bool{}

	for _, f := range files {
		age := now.Sub(f.CreatedAt)
		switch {
		case age < policy.KeepAllWithin:
			// Recent tier: keep every backup.
			keep[f.Name] = true
		case age < policy.WeeklyUntil:
			// Weekly tier: bucket by ISO year-week.
			y, w := f.CreatedAt.ISOWeek()
			key := fmt.Sprintf("w-%04d-%02d", y, w)
			if !seenBucket[key] {
				seenBucket[key] = true
				keep[f.Name] = true
			}
		default:
			// Monthly tier: bucket by calendar year-month.
			key := fmt.Sprintf("m-%04d-%02d", f.CreatedAt.Year(), int(f.CreatedAt.Month()))
			if !seenBucket[key] {
				seenBucket[key] = true
				keep[f.Name] = true
			}
		}
	}

	var removed []string
	for _, f := range files {
		if keep[f.Name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, f.Name)); err != nil {
			return removed, fmt.Errorf("remove %s: %w", f.Name, err)
		}
		removed = append(removed, f.Name)
	}
	return removed, nil
}
