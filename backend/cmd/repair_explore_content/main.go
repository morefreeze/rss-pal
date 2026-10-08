// repair_explore_content normalizes legacy Explore cache and exact imported
// copies. Dry-run is the default; --apply requires an exclusive backup file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "apply repairs (default is read-only dry-run)")
	backupPath := flag.String("backup", "", "new JSONL backup path, required with --apply")
	flag.Parse()
	opts := explore.ContentRepairOptions{Apply: *apply}
	if *apply {
		if *backupPath == "" {
			return fmt.Errorf("--apply requires --backup")
		}
		file, err := os.OpenFile(*backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		directory, err := os.Open(filepath.Dir(*backupPath))
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		encoder := json.NewEncoder(file)
		opts.Backup = func(b explore.ContentRepairBackup) error {
			if err := encoder.Encode(b); err != nil {
				return err
			}
			return file.Sync()
		}
	}
	cfg := config.Load()
	db, err := repository.NewBypassDB(&cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	opts.Progress = func(s explore.ContentRepairStats) {
		if s.Scanned%500 == 0 {
			log.Printf("progress: %+v", s)
		}
	}
	stats, err := explore.RepairContent(ctx, db, opts)
	log.Printf("apply=%t result=%+v", *apply, stats)
	return err
}
