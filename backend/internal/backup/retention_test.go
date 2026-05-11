package backup

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// makeFiles drops one empty backup file per timestamp into a fresh temp dir.
// Returns the dir path so the test can pass it to Prune.
func makeFiles(t *testing.T, times []time.Time) string {
	t.Helper()
	dir := t.TempDir()
	for _, ts := range times {
		name := fileNamePrefix + ts.UTC().Format(fileTimeLayout) + fileNameSuffix
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	files, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestParseFilename(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"rss-pal-backup-20260511-192357.json", time.Date(2026, 5, 11, 19, 23, 57, 0, time.UTC), true},
		{"rss-pal-backup-20260101-000000.json", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"random.json", time.Time{}, false},
		{"rss-pal-backup-bad.json", time.Time{}, false},
		{"rss-pal-backup-20260511-192357.txt", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := parseFilename(c.in)
		if ok != c.ok {
			t.Errorf("parseFilename(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("parseFilename(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPruneKeepsAllRecent(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	// Five backups, all in the last 7 days. Every one should survive.
	times := []time.Time{
		now.Add(-1 * time.Hour),
		now.Add(-25 * time.Hour),
		now.Add(-3 * 24 * time.Hour),
		now.Add(-5 * 24 * time.Hour),
		now.Add(-6*24*time.Hour - 23*time.Hour), // just under 7d
	}
	dir := makeFiles(t, times)

	removed, err := Prune(dir, now, DefaultRetention)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removals in recent tier, got %v", removed)
	}
	if got := len(listNames(t, dir)); got != 5 {
		t.Errorf("expected 5 files remaining, got %d", got)
	}
}

func TestPruneWeeklyTier(t *testing.T) {
	// Three backups in the same ISO week, age 10-12 days. Only the newest
	// (smallest age) should survive the weekly bucket.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC) // a Monday
	times := []time.Time{
		now.Add(-10 * 24 * time.Hour), // Fri 2 weeks back, age 10d
		now.Add(-11 * 24 * time.Hour), // Thu, age 11d
		now.Add(-12 * 24 * time.Hour), // Wed, age 12d (newest of bucket? no — smallest age wins)
	}
	dir := makeFiles(t, times)

	if _, err := Prune(dir, now, DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	got := listNames(t, dir)
	// Files in the same ISO week collapse to 1.
	weeks := map[string]bool{}
	for _, n := range got {
		ts, _ := parseFilename(n)
		y, w := ts.ISOWeek()
		key := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "-w" + time.Date(0, 0, w, 0, 0, 0, 0, time.UTC).Format("01")
		if weeks[key] {
			t.Errorf("two backups survived in same ISO week: %v", got)
		}
		weeks[key] = true
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file in single week bucket, got %d: %v", len(got), got)
	}
}

func TestPruneMonthlyTier(t *testing.T) {
	// Five backups spread over Jan 2026 (>30 days old vs reference May 2026).
	// Should collapse to 1.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 18, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	}
	dir := makeFiles(t, times)

	if _, err := Prune(dir, now, DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	got := listNames(t, dir)
	if len(got) != 1 {
		t.Errorf("expected 1 file in Jan 2026 bucket, got %d: %v", len(got), got)
	}
	// The survivor is the newest in the month (Jan 30).
	ts, _ := parseFilename(got[0])
	if ts.Day() != 30 {
		t.Errorf("survivor should be Jan 30, got day %d", ts.Day())
	}
}

func TestPruneMixedTiers(t *testing.T) {
	// Comprehensive: 2 recent, 3 in same week 10-12d ago, 5 in same month
	// 60-90d ago. Should end up with 2 + 1 + 1 = 4 files.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		// recent
		now.Add(-2 * time.Hour),
		now.Add(-3 * 24 * time.Hour),
		// weekly bucket (~10-12 days old)
		now.Add(-10 * 24 * time.Hour),
		now.Add(-11 * 24 * time.Hour),
		now.Add(-12 * 24 * time.Hour),
		// monthly bucket (>30d, all in Feb 2026)
		time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC),
	}
	dir := makeFiles(t, times)

	removed, err := Prune(dir, now, DefaultRetention)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	got := listNames(t, dir)
	if len(got) != 4 {
		t.Errorf("expected 4 survivors (2 recent + 1 weekly + 1 monthly), got %d: %v\nremoved: %v",
			len(got), got, removed)
	}
}

func TestPruneAlwaysKeepsNewest(t *testing.T) {
	// Single very old backup — should be preserved as the newest-overall
	// safety net.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	dir := makeFiles(t, times)

	if _, err := Prune(dir, now, DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got := len(listNames(t, dir)); got != 1 {
		t.Errorf("the only existing backup should not be deleted, got %d files", got)
	}
}

func TestPruneEmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := Prune(dir, time.Now(), DefaultRetention)
	if err != nil {
		t.Fatalf("Prune empty: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("unexpected removals on empty dir: %v", removed)
	}
}

func TestPruneMissingDir(t *testing.T) {
	// Non-existent dir should be a no-op (treated like empty).
	removed, err := Prune(filepath.Join(t.TempDir(), "does-not-exist"), time.Now(), DefaultRetention)
	if err != nil {
		t.Fatalf("Prune missing: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("unexpected removals on missing dir: %v", removed)
	}
}

func TestPruneIgnoresUnrelatedFiles(t *testing.T) {
	dir := makeFiles(t, []time.Time{time.Now()})
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rss-pal-backup-junk.json"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(dir, time.Now(), DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// Both files should still be there — List skipped them, so Prune did too.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("expected unrelated files preserved, got %d entries", len(entries))
	}
}
