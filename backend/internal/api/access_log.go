package api

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// RedactedAccessLogger logs only request metadata that cannot contain bearer
// values from the URL. Route templates are used instead of concrete paths.
func RedactedAccessLogger(output io.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "<unmatched>"
		}
		_, _ = fmt.Fprintf(
			output,
			"timestamp=%s status=%d latency=%s client_ip=%s method=%s path=%s\n",
			time.Now().UTC().Format(time.RFC3339Nano),
			c.Writer.Status(),
			time.Since(start),
			c.ClientIP(),
			c.Request.Method,
			path,
		)
	}
}

// RedactedRecovery recovers panics without logging the recovered value or
// request data. The stack identifies the failing code without exposing inputs.
func RedactedRecovery(output io.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() == nil {
				return
			}
			_, _ = fmt.Fprintf(output, "panic recovered\n%s", debug.Stack())
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}
