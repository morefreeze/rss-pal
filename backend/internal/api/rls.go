package api

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/gin-gonic/gin"
)

// CtxKeyTx is the gin context key under which the per-request *sql.Tx is
// stored. Aliases ctxkey.Tx so the api package and repository WithCtx
// helpers share a single source of truth for the string literal.
const CtxKeyTx = ctxkey.Tx

// RLSTxMiddleware opens a per-request transaction, sets app.user_id (and
// app.is_admin if applicable) via SET LOCAL, and exposes the tx via the gin
// context. The transaction is committed on success (HTTP status < 500) or
// rolled back on 5xx / panic. Must run AFTER AuthMiddleware.
func RLSTxMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uidRaw, exists := c.Get("userID")
		if !exists {
			log.Printf("rls: userID missing from context after AuthMiddleware")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal: missing user context"})
			return
		}
		userID, ok := uidRaw.(int)
		if !ok {
			log.Printf("rls: userID context value has wrong type %T", uidRaw)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal: bad user context"})
			return
		}
		isAdmin := c.GetBool("isAdmin")

		tx, err := db.BeginTx(c.Request.Context(), nil)
		if err != nil {
			log.Printf("rls: BeginTx: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx begin"})
			return
		}
		// set_config(name, value, is_local=true) is equivalent to SET LOCAL
		// but accepts a parameterised value.
		if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
			log.Printf("rls: set app.user_id for user %d: %v", userID, err)
			_ = tx.Rollback()
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
			return
		}
		if isAdmin {
			if _, err := tx.Exec(`SELECT set_config('app.is_admin', 'true', true)`); err != nil {
				log.Printf("rls: set app.is_admin: %v", err)
				_ = tx.Rollback()
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
				return
			}
		}

		c.Set(CtxKeyTx, tx)

		defer func() {
			if rec := recover(); rec != nil {
				_ = tx.Rollback()
				panic(rec) // re-raise so gin's recovery middleware logs it
			}
		}()

		c.Next()

		// Treat any handler-recorded error as a rollback signal. Handlers in this
		// codebase do not use c.Error() for telemetry or validation logging — only
		// for genuine failures. If that convention changes, this check must change
		// with it, or commits will silently roll back.
		if c.Writer.Status() >= 500 || len(c.Errors) > 0 {
			_ = tx.Rollback()
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("CRITICAL: rls: tx.Commit failed after %d response: %v", c.Writer.Status(), err)
			_ = c.Error(err)
		}
	}
}
