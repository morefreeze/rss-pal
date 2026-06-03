package api

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrPublicTokenInvalid is returned by a PublicTokenResolver when the
// supplied token is missing, malformed, or does not match a known owner.
// The middleware converts it to a 401 response.
var ErrPublicTokenInvalid = errors.New("public token invalid")

// PublicTokenResolver inspects the request and the open tx to determine
// the owning user_id. Resolvers may run non-RLS queries on tx (e.g.
// share_tokens, users.bookmarklet_token) before app.user_id is set.
// Return ErrPublicTokenInvalid to surface a 401; any other error becomes
// a 500. A zero/negative uid is also treated as invalid.
type PublicTokenResolver func(c *gin.Context, tx *sql.Tx) (userID int, err error)

// PublicTokenMiddleware wraps a public-token endpoint in a request tx
// whose app.user_id is set to the token-resolved owner, then stashes the
// tx under CtxKeyTx so repository.WithCtx picks it up just like JWT
// routes do via RLSTxMiddleware.
//
// Commit/rollback policy mirrors RLSTxMiddleware:
//   - Commit when no panic, c.Writer.Status() < 500, and c.Errors is empty.
//   - Roll back on 5xx, c.Error, or panic; re-raise panic.
//
// Resolver failures (ErrPublicTokenInvalid) short-circuit with 401 and
// roll back the tx.
func PublicTokenMiddleware(db *sql.DB, resolve PublicTokenResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		tx, err := db.BeginTx(c.Request.Context(), nil)
		if err != nil {
			log.Printf("public_token: BeginTx: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx begin"})
			return
		}
		uid, err := resolve(c, tx)
		if err != nil {
			_ = tx.Rollback()
			if errors.Is(err, ErrPublicTokenInvalid) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
			log.Printf("public_token: resolve: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "resolve"})
			return
		}
		if uid <= 0 {
			_ = tx.Rollback()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		if err := setTxUserID(tx, uid); err != nil {
			log.Printf("public_token: set app.user_id for user %d: %v", uid, err)
			_ = tx.Rollback()
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
			return
		}

		// Stash for handler-side WithCtx and for getUserID(c) consistency.
		c.Set(CtxKeyTx, tx)
		c.Set("userID", uid)

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
			log.Printf("CRITICAL: public_token: tx.Commit failed after %d response: %v", c.Writer.Status(), err)
			_ = c.Error(err)
		}
	}
}

// setTxUserID applies SET LOCAL app.user_id on the supplied tx. Exposed
// at package scope so per-route resolvers can reuse the exact same SQL
// idiom if they ever need it.
func setTxUserID(tx *sql.Tx, uid int) error {
	_, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, uid)
	return err
}

// bearerToken extracts the bearer credential from an Authorization
// header. Accepts both "Bearer <tok>" and a bare token (existing
// bookmarklet/extension clients have used both shapes historically).
// Returns "" when the header is absent or has only whitespace.
func bearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	token := authHeader
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
		token = authHeader[len(prefix):]
	}
	for len(token) > 0 && (token[0] == ' ' || token[0] == '\t') {
		token = token[1:]
	}
	for len(token) > 0 && (token[len(token)-1] == ' ' || token[len(token)-1] == '\t') {
		token = token[:len(token)-1]
	}
	return token
}
