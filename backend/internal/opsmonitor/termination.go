package opsmonitor

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// InstallTerminationHandler is explicitly enabled by the two executable entry
// points. Routine container SIGTERM drains buffered telemetry before exit. A
// SIGKILL, crash or database outage can still lose the current minute's samples.
// quiesce should stop incoming requests; its context is bounded to three seconds.
func (r *Recorder) InstallTerminationHandler(quiesce func(context.Context)) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	stopped := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { signal.Stop(signals); close(stopped) }) }
	go func() {
		select {
		case <-stopped:
			return
		case <-signals:
			if quiesce != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				quiesce(ctx)
				cancel()
			}
			r.Close()
			os.Exit(0)
		}
	}()
	return stop
}
