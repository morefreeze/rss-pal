package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// AdminPreviewOnly hides unfinished routes from readers and checks the current
// database role so a JWT issued before a role change cannot grant access.
func AdminPreviewOnly(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		var admin bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND is_admin)`, getUserID(c)).Scan(&admin)
		switch {
		case err != nil:
			_ = c.Error(err)
			c.AbortWithStatus(http.StatusServiceUnavailable)
		case !admin:
			c.AbortWithStatus(http.StatusNotFound)
		default:
			c.Next()
		}
	}
}
