package api

import (
	gingzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// ResponseCompression keeps streaming routes outside the gzip writer, whose
// underlying Flush does not flush compressed application frames.
func ResponseCompression() gin.HandlerFunc {
	return gingzip.Gzip(
		gingzip.DefaultCompression,
		gingzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".mp4", ".mp3", ".woff", ".woff2"}),
		gingzip.WithExcludedPathsRegexs([]string{
			// Streaming and JSON summaries share this path (?stream=1).
			"/api/articles/.*/summary$",
			"/api/articles/.*/summary/stream",
			"/api/media/youtube/.*",
		}),
	)
}
