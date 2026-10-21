package api

import "github.com/gin-gonic/gin"

const afterCommitKey = "rss:after-commit"

// afterCommit defers background work until the RLS transaction is durable.
func afterCommit(c *gin.Context, fn func()) {
	if _, ok := c.Get(CtxKeyTx); !ok {
		fn()
		return
	}
	raw, _ := c.Get(afterCommitKey)
	callbacks, _ := raw.([]func())
	c.Set(afterCommitKey, append(callbacks, fn))
}
func runAfterCommit(c *gin.Context) {
	raw, _ := c.Get(afterCommitKey)
	callbacks, _ := raw.([]func())
	for _, fn := range callbacks {
		fn()
	}
}

const afterRollbackKey = "rss:after-rollback"

// afterRollback runs only after a transaction has been rolled back or failed
// commit. It also handles streams, whose HTTP headers are already sent.
func afterRollback(c *gin.Context, fn func()) {
	if _, ok := c.Get(CtxKeyTx); !ok {
		return
	}
	raw, _ := c.Get(afterRollbackKey)
	callbacks, _ := raw.([]func())
	c.Set(afterRollbackKey, append(callbacks, fn))
}
func runAfterRollback(c *gin.Context) {
	raw, _ := c.Get(afterRollbackKey)
	callbacks, _ := raw.([]func())
	for _, fn := range callbacks {
		fn()
	}
}
