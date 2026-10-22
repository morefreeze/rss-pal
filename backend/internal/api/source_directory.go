package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/sourcedirectory"
	"github.com/gin-gonic/gin"
)

type SourceDirectoryHandler struct{ directory *sourcedirectory.Service }

func NewSourceDirectoryHandler(directory *sourcedirectory.Service) *SourceDirectoryHandler {
	return &SourceDirectoryHandler{directory: directory}
}
func (h *SourceDirectoryHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "private, max-age=300")
	c.JSON(http.StatusOK, h.directory.Get())
}
