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
