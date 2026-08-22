package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const workerHeartbeatThreshold = 3 * time.Minute

// HeartbeatReader retrieves the last heartbeat for a service component.
type HeartbeatReader interface {
	LastSeen(component string) (time.Time, error)
}

// SystemHealthHandler exposes status checks for internal system components.
type SystemHealthHandler struct {
	reader HeartbeatReader
	now    func() time.Time
}

func NewSystemHealthHandler(reader HeartbeatReader, now func() time.Time) *SystemHealthHandler {
	return &SystemHealthHandler{reader: reader, now: now}
}

// Worker reports whether the background worker has emitted a recent heartbeat.
func (h *SystemHealthHandler) Worker(c *gin.Context) {
	lastSeen, err := h.reader.LastSeen("worker")
	if err != nil || h.now().Sub(lastSeen) > workerHeartbeatThreshold {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "down"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
