package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const committedJSONKey = "rss:committed-json"

type committedJSON struct {
	status int
	body   any
}

// jsonAfterCommit is opt-in for non-streaming mutation handlers whose success
// response asserts durable persistence. The request middleware owns commit;
// unrelated handlers retain their existing streaming/response behavior.
func jsonAfterCommit(c *gin.Context, status int, body any) {
	if _, ok := c.Get(CtxKeyTx); !ok {
		c.JSON(status, body)
		return
	}
	c.Set(committedJSONKey, committedJSON{status: status, body: body})
}

func writeCommittedJSON(c *gin.Context) {
	if raw, ok := c.Get(committedJSONKey); ok {
		response := raw.(committedJSON)
		c.JSON(response.status, response.body)
	}
}

func rejectCommittedJSON(c *gin.Context) {
	if _, ok := c.Get(committedJSONKey); ok && !c.Writer.Written() {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败，请重试；更改尚未确认保存"})
	}
}
