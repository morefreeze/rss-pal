// Command backup_migrate packs legacy backup files into the canonical
// .tar.gz format that the rest of the system writes going forward.
//
// For each `rss-pal-backup-<ts>.json` in the target directory:
//   - if a paired `rss-pal-backup-<ts>.saved.json.gz` sibling exists, both
//     are bundled into one `rss-pal-backup-<ts>.tar.gz`
//   - otherwise the lone .json is wrapped in a single-member .tar.gz
//
// The new file is verified by extracting and Load-ing both members back out
// before any originals are deleted. The original timestamp stamp in the
// filename is preserved, so List ordering and retention bucketing don't
// shift.
//
// Flags:
//
//	-dir       directory to migrate (default /backups)
//	-dry-run   report what would change, write nothing
//	-keep      keep the originals after a successful migration
//
// Re-running is safe: files already in .tar.gz form are skipped, and the
// per-stamp pre-check refuses to overwrite an existing target.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/backup"
)

const (
	fileNamePrefix    = "rss-pal-backup-"
	jsonSuffix        = ".json"
	savedSuffix       = ".saved.json.gz"
	tarballSuffix     = ".tar.gz"
	fileTimeLayout    = "20060102-150405"
)

type item struct {
	stamp     time.Time
	metaName  string
	savedName string // empty if no sibling
}

func main() {
	dir := flag.String("dir", "/backups", "backup directory to migrate")
	dryRun := flag.Bool("dry-run", false, "report only; do not write or delete")
	keep := flag.Bool("keep", false, "keep originals after a successful migration")
	flag.Parse()

	items, err := scan(*dir)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}
	if len(items) == 0 {
		log.Printf("no legacy backups to migrate in %s", *dir)
		return
	}

	log.Printf("found %d legacy backup(s) to migrate:", len(items))
	for _, it := range items {
		paired := "solo"
		if it.savedName != "" {
			paired = "paired"
		}
		log.Printf("  - %s (%s)", it.metaName, paired)
	}

	if *dryRun {
		log.Printf("dry-run: no changes written")
		return
	}

	var migrated, failed int
	for _, it := range items {
		if err := migrateOne(*dir, it, *keep); err != nil {
			log.Printf("FAIL %s: %v", it.metaName, err)
			failed++
			continue
		}
		log.Printf("OK   %s -> %s%s%s", it.metaName, fileNamePrefix, it.stamp.UTC().Format(fileTimeLayout), tarballSuffix)
		migrated++
	}
	log.Printf("done: %d migrated, %d failed", migrated, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// scan returns legacy entries needing migration. Entries already present as
// .tar.gz at the same timestamp are skipped (re-run friendly).
func scan(dir string) ([]item, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	hasTarball := map[time.Time]bool{}
	siblings := map[string]string{} // metaName -> savedName
	var metas []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, fileNamePrefix) {
			continue
		}
		core := strings.TrimPrefix(n, fileNamePrefix)
		switch {
		case strings.HasSuffix(core, tarballSuffix):
			if ts, ok := parseStamp(strings.TrimSuffix(core, tarballSuffix)); ok {
				hasTarball[ts] = true
			}
		case strings.HasSuffix(core, savedSuffix):
			stamp := strings.TrimSuffix(core, savedSuffix)
			meta := fileNamePrefix + stamp + jsonSuffix
			siblings[meta] = n
		case strings.HasSuffix(core, jsonSuffix):
			metas = append(metas, n)
		}
	}
	sort.Strings(metas)

	var out []item
	for _, m := range metas {
		stamp, ok := parseStamp(strings.TrimSuffix(strings.TrimPrefix(m, fileNamePrefix), jsonSuffix))
		if !ok {
			continue
		}
		if hasTarball[stamp] {
			continue
		}
		out = append(out, item{stamp: stamp, metaName: m, savedName: siblings[m]})
	}
	return out, nil
}

func parseStamp(core string) (time.Time, bool) {
	t, err := time.ParseInLocation(fileTimeLayout, core, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func migrateOne(dir string, it item, keep bool) error {
	targetName := fileNamePrefix + it.stamp.UTC().Format(fileTimeLayout) + tarballSuffix
	targetPath := filepath.Join(dir, targetName)
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("target %s already exists, refuse to overwrite", targetName)
	}

	metaPath := filepath.Join(dir, it.metaName)
	s, err := backup.Load(metaPath)
	if err != nil {
		return fmt.Errorf("load source metadata: %w", err)
	}

	// LoadSaved is no-op-on-missing for legacy solo, so we don't need to
	// branch on it.savedName here.
	ss, err := backup.LoadSaved(metaPath)
	if err != nil {
		return fmt.Errorf("load source saved sibling: %w", err)
	}
	if ss == nil {
		// Wrap the empty saved-snapshot so the on-disk tarball always has a
		// .saved.json.gz member — consistent with what WriteFiles produces
		// for fresh backups.
		ss = &backup.SavedSnapshot{
			Version:   backup.SavedSnapshotVersion,
			CreatedAt: s.CreatedAt,
		}
	}

	// Re-stamp the snapshot CreatedAt to the source's so WriteFiles names the
	// target with the same timestamp (filename derives from s.CreatedAt).
	// Load already populated it; just be defensive.
	if s.CreatedAt.IsZero() {
		s.CreatedAt = it.stamp.UTC()
	}
	if ss.CreatedAt.IsZero() {
		ss.CreatedAt = s.CreatedAt
	}

	if _, _, err := backup.WriteFiles(s, ss, dir); err != nil {
		return fmt.Errorf("write tarball: %w", err)
	}

	// Verify by extracting and parsing back out.
	tmpDir, err := os.MkdirTemp("", "rss-pal-migrate-verify-*")
	if err != nil {
		return fmt.Errorf("mkdir verify tmp: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	vmeta := filepath.Join(tmpDir, "verify.json")
	vsaved := filepath.Join(tmpDir, "verify.saved.json.gz")
	if _, err := backup.ExtractTarball(targetPath, vmeta, vsaved); err != nil {
		os.Remove(targetPath)
		return fmt.Errorf("verify extract: %w", err)
	}
	if _, err := backup.Load(vmeta); err != nil {
		os.Remove(targetPath)
		return fmt.Errorf("verify metadata: %w", err)
	}
	if _, err := backup.LoadSaved(vmeta); err != nil {
		os.Remove(targetPath)
		return fmt.Errorf("verify saved: %w", err)
	}

	if keep {
		return nil
	}
	if err := os.Remove(metaPath); err != nil {
		return fmt.Errorf("remove source metadata: %w", err)
	}
	if it.savedName != "" {
		if err := os.Remove(filepath.Join(dir, it.savedName)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove source saved sibling: %w", err)
		}
	}
	return nil
}
