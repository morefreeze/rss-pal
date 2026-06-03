package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/gin-gonic/gin"
)

// bestEffort runs fn inside a SAVEPOINT on the per-request tx. If fn returns
// an error, the savepoint is rolled back (which leaves the outer tx in a
// usable state, unlike a bare error from inside an outer tx). The error is
// logged with the supplied label and otherwise discarded.
//
// Use ONLY for genuinely best-effort writes (cache fills, signal weight
// adjustments, auto-titling) where failure should not cause the user-facing
// request to fail. For required writes, propagate the error normally.
//
// If no request tx is in context (worker, pre-auth route), bestEffort runs
// fn directly without a savepoint — the outer tx contract does not apply.
func bestEffort(c *gin.Context, label string, fn func() error) {
	v, ok := c.Get(ctxkey.Tx)
	if !ok {
		if err := fn(); err != nil {
			log.Printf("best-effort %s: %v", label, err)
		}
		return
	}
	q, ok := v.(repository.Querier)
	if !ok {
		if err := fn(); err != nil {
			log.Printf("best-effort %s (non-Querier tx): %v", label, err)
		}
		return
	}
	var nameBytes [8]byte
	_, _ = rand.Read(nameBytes[:])
	name := "sp_" + hex.EncodeToString(nameBytes[:])
	if _, err := q.Exec("SAVEPOINT " + name); err != nil {
		log.Printf("best-effort %s: savepoint open: %v", label, err)
		// Outer tx may already be aborted; run fn anyway so logs at least
		// capture the underlying failure.
		if err := fn(); err != nil {
			log.Printf("best-effort %s: %v", label, err)
		}
		return
	}
	if err := fn(); err != nil {
		log.Printf("best-effort %s: %v", label, err)
		if _, rerr := q.Exec("ROLLBACK TO SAVEPOINT " + name); rerr != nil {
			log.Printf("best-effort %s: rollback savepoint: %v", label, rerr)
		}
		return
	}
	if _, err := q.Exec("RELEASE SAVEPOINT " + name); err != nil {
		log.Printf("best-effort %s: release savepoint: %v", label, err)
	}
}
