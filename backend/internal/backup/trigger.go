package backup

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// DebounceQuietPeriod is how long the debouncer waits after the last Trigger
// call before actually writing a backup. Rapid feed adds/deletes coalesce
// into one snapshot.
const DebounceQuietPeriod = 5 * time.Minute

// Runner is a thread-safe owner of a backup directory and the debounce timer
// that drives change-triggered snapshots. Each process (server, worker) makes
// its own Runner; the file format is shared so all snapshots from any process
// end up in the same directory.
type Runner struct {
	db  *sql.DB
	dir string

	mu    sync.Mutex
	timer *time.Timer

	// inflight prevents a daily tick and a debounced fire from running
	// concurrently — both would succeed but it's wasteful.
	inflight sync.Mutex
}

// NewRunner constructs a Runner. dir is created on first write.
func NewRunner(db *sql.DB, dir string) *Runner {
	return &Runner{db: db, dir: dir}
}

// TriggerAsync schedules a backup to run after DebounceQuietPeriod of
// quiescence. Repeated calls within the quiet window reset the timer so a
// burst of changes produces exactly one snapshot.
func (r *Runner) TriggerAsync() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(DebounceQuietPeriod, func() {
		if err := r.RunNow(context.Background()); err != nil {
			log.Printf("backup: debounced run failed: %v", err)
		}
	})
}

// RunNow builds + writes a snapshot and applies retention. Safe to call
// concurrently with TriggerAsync (the inflight lock serializes).
func (r *Runner) RunNow(ctx context.Context) error {
	r.inflight.Lock()
	defer r.inflight.Unlock()

	s, ss, err := Build(ctx, r.db)
	if err != nil {
		return err
	}
	metaPath, _, err := WriteFiles(s, ss, r.dir)
	if err != nil {
		return err
	}
	removed, err := Prune(r.dir, time.Now(), DefaultRetention)
	if err != nil {
		log.Printf("backup: wrote %s but prune failed: %v", metaPath, err)
		return nil
	}
	if len(removed) > 0 {
		log.Printf("backup: wrote %s (+saved sibling), pruned %d old files", metaPath, len(removed))
	} else {
		log.Printf("backup: wrote %s (+saved sibling)", metaPath)
	}
	return nil
}

// ScheduleDaily starts a ticker that runs the backup every 24h. Returns a
// stop function. If the directory is empty on startup it also fires once
// immediately so a fresh deployment doesn't wait a day for its first backup.
func (r *Runner) ScheduleDaily(ctx context.Context) (stop func()) {
	files, _ := List(r.dir)
	if len(files) == 0 {
		go func() {
			if err := r.RunNow(ctx); err != nil {
				log.Printf("backup: initial run failed: %v", err)
			}
		}()
	}

	t := time.NewTicker(24 * time.Hour)
	doneCh := make(chan struct{})
	go func() {
		for {
			select {
			case <-t.C:
				if err := r.RunNow(ctx); err != nil {
					log.Printf("backup: daily run failed: %v", err)
				}
			case <-doneCh:
				t.Stop()
				return
			}
		}
	}()
	return func() { close(doneCh) }
}
