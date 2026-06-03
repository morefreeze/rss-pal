package api

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CtxKeyTx is the gin context key under which the per-request *sql.Tx is stored.
const CtxKeyTx = "tx"

// RLSTxMiddleware opens a per-request transaction, sets app.user_id (and
// app.is_admin if applicable) via SET LOCAL, and exposes the tx via the gin
// context. The transaction is committed on success (HTTP status < 500) or
// rolled back on 5xx / panic. Must run AFTER AuthMiddleware.
func RLSTxMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		uidRaw, exists := c.Get("userID")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing user"})
			return
		}
		userID, _ := uidRaw.(int)
		isAdmin := c.GetBool("isAdmin")

		tx, err := db.BeginTx(c.Request.Context(), nil)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx begin"})
			return
		}
		// set_config(name, value, is_local=true) is equivalent to SET LOCAL
		// but accepts a parameterised value.
		if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
			_ = tx.Rollback()
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
			return
		}
		if isAdmin {
			if _, err := tx.Exec(`SELECT set_config('app.is_admin', 'true', true)`); err != nil {
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

		if c.Writer.Status() >= 500 || len(c.Errors) > 0 {
			_ = tx.Rollback()
			return
		}
		if err := tx.Commit(); err != nil {
			_ = c.Error(err)
		}
	}
}
