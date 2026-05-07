package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/gin-gonic/gin"
)

func (h *InsightsHandler) GenerateStream(c *gin.Context) {
	userID := getUserID(c)

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	emit := func(payload any) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		c.Writer.Write(b)
		c.Writer.Write([]byte("\n"))
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}

	quota, ok := h.computeQuota(userID)
	if !ok {
		emit(map[string]any{
			"type":            "error",
			"msg":             "quota_exceeded",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	topics, err := h.prefRepo.GetTopicStrings(userID)
	if err != nil || len(topics) == 0 {
		emit(map[string]any{
			"type":            "error",
			"msg":             "no_data",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}
	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)

	summarizer := h.chooseSummarizer(userID)
	prompt := buildInsightStreamPrompt(topics, titles)

	var full strings.Builder
	_, err = streamCall(c, summarizer, prompt, func(delta string) {
		full.WriteString(delta)
		emit(map[string]any{"type": "delta", "text": delta})
	})
	if err != nil {
		emit(map[string]any{"type": "error", "msg": err.Error()})
		return
	}

	if err := h.userInsightsRepo.Insert(userID, full.String(), "manual", summarizer.Model()); err != nil {
		// Content already streamed; signal save failure as an error frame.
		emit(map[string]any{"type": "error", "msg": "save_failed: " + err.Error()})
		return
	}

	quota, _ = h.computeQuota(userID)
	emit(map[string]any{
		"type":            "done",
		"full":            full.String(),
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

// streamCall invokes Summarizer.CallStream; isolated for clarity & testability.
func streamCall(c *gin.Context, s *ai.Summarizer, prompt string, onDelta func(string)) (string, error) {
	return s.CallStream(c.Request.Context(), prompt, 1500, onDelta)
}

func buildInsightStreamPrompt(topics, titles []string) string {
	return "基于用户的兴趣主题和最近阅读，请用中文 markdown 给出洞察分析（核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）：\n\n" +
		"## 用户兴趣主题（按权重排序）\n" + strings.Join(topics, "\n") + "\n\n" +
		"## 最近阅读的文章标题\n" + strings.Join(titles, "\n")
}
