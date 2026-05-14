package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// First: sweep any orphan saved-archive files (no matching metadata).
	// They can result from a crash between WriteSavedFile and WriteFile, or
	// from a half-finished prune.
	if err := sweepOrphanSavedFiles(dir); err != nil {
		return nil, err
	}

	files, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool { return files[i].CreatedAt.After(files[j].CreatedAt) })

	keep := map[string]bool{files[0].Name: true}
	seenBucket := map[string]bool{}

	for _, f := range files {
		age := now.Sub(f.CreatedAt)
		switch {
		case age < policy.KeepAllWithin:
			keep[f.Name] = true
		case age < policy.WeeklyUntil:
			y, w := f.CreatedAt.ISOWeek()
			key := fmt.Sprintf("w-%04d-%02d", y, w)
			if !seenBucket[key] {
				seenBucket[key] = true
				keep[f.Name] = true
			}
		default:
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
		metaPath := filepath.Join(dir, f.Name)
		if err := os.Remove(metaPath); err != nil {
			return removed, fmt.Errorf("remove %s: %w", f.Name, err)
		}
		removed = append(removed, f.Name)
		// Best-effort sibling delete. Missing sibling (legacy backup) is fine.
		savedPath := savedSiblingPath(metaPath)
		if err := os.Remove(savedPath); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("remove sibling %s: %w", filepath.Base(savedPath), err)
		}
	}
	return removed, nil
}

// sweepOrphanSavedFiles deletes any *.saved.json.gz whose paired metadata
// file is absent. Called at the top of Prune.
func sweepOrphanSavedFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	hasMeta := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := parseFilename(e.Name()); ok {
			hasMeta[e.Name()] = true
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, savedFileSuffix) {
			continue
		}
		metaName := strings.TrimSuffix(name, savedFileSuffix) + fileNameSuffix
		if hasMeta[metaName] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("sweep orphan %s: %w", name, err)
		}
	}
	return nil
}
