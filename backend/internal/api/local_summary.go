package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func localSummaryHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// LocalSummaryInput only prepares prompts; a GET never calls a model service.
func (h *ArticleHandler) LocalSummaryInput(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	article, err := h.articleRepo.WithCtx(c).GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(404, gin.H{"error": "article not found"})
		return
	}
	if strings.TrimSpace(article.Content) == "" || lacksSummarizableMediaContent(article) {
		c.JSON(422, gin.H{"error": "暂无可总结的正文或字幕，请先获取正文后再手动生成"})
		return
	}
	briefTemplate, detailedTemplate := "", ""
	if template := c.Query("template_id"); template != "" {
		tid, terr := strconv.Atoi(template)
		if terr != nil || tid <= 0 || h.templateRepo == nil {
			c.JSON(400, gin.H{"error": "invalid template"})
			return
		}
		tpl, terr := h.templateRepo.WithCtx(c).GetByID(tid)
		if terr != nil || tpl == nil || (!tpl.IsSystem && (tpl.UserID == nil || *tpl.UserID != getUserID(c))) {
			c.JSON(404, gin.H{"error": "template not found"})
			return
		}
		briefTemplate, detailedTemplate = tpl.BriefPrompt, tpl.DetailedPrompt
	}
	brief, detailed := ai.LocalSummaryPrompts(article.Title, article.Content, briefTemplate, detailedTemplate)
	c.JSON(200, gin.H{"content_hash": localSummaryHash(article.Content), "summary_version": localSummaryHash(article.SummaryBrief + "\x00" + article.SummaryDetailed), "brief_prompt": brief, "detailed_prompt": detailed})
}

// SaveLocalSummary receives an allowlist of results, never configuration or keys.
func (h *ArticleHandler) SaveLocalSummary(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	repo := h.articleRepo.WithCtx(c)
	article, err := repo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(404, gin.H{"error": "article not found"})
		return
	}
	var result struct {
		ContentHash    string `json:"content_hash"`
		SummaryVersion string `json:"summary_version"`
		Brief          string `json:"summary_brief"`
		Detailed       string `json:"summary_detailed"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 200000)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		c.JSON(400, gin.H{"error": "仅接受摘要结果，不能包含 API Key 或配置"})
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		c.JSON(400, gin.H{"error": "invalid result"})
		return
	}
	if strings.TrimSpace(result.Brief) == "" || strings.TrimSpace(result.Detailed) == "" || utf8.RuneCountInString(result.Brief) > 20000 || utf8.RuneCountInString(result.Detailed) > 40000 {
		c.JSON(400, gin.H{"error": "摘要为空或过长，请检查模型输出"})
		return
	}
	if result.ContentHash != localSummaryHash(article.Content) {
		c.JSON(409, gin.H{"error": staleSummaryRetryMessage})
		return
	}
	if result.SummaryVersion != localSummaryHash(article.SummaryBrief+"\x00"+article.SummaryDetailed) {
		c.JSON(409, gin.H{"error": "摘要已被其他任务更新，请刷新文章后重新生成"})
		return
	}
	token, err := h.articleRepo.ClaimSummary(c.Request.Context(), id, true)
	if err != nil {
		if errors.Is(err, repository.ErrSummaryInProgress) {
			c.JSON(409, gin.H{"error": "文章正在保存或生成总结，请稍后重试"})
		} else {
			c.JSON(503, gin.H{"error": "摘要保存暂不可用，请稍后重试"})
		}
		return
	}
	settled := false
	fail := func() {
		if !settled {
			_ = h.articleRepo.FailSummary(id, token, errors.New("本地摘要保存未完成"))
		}
	}
	if _, hasTx := c.Get(CtxKeyTx); hasTx {
		afterCommit(c, fail)
	} else {
		defer fail()
	}
	afterRollback(c, func() { _ = h.articleRepo.FailSummary(id, token, errors.New("本地摘要提交失败")) })
	saved, err := repo.CompleteLocalSummary(id, token, article.Content, result.Brief, result.Detailed, article.SummaryBrief, article.SummaryDetailed)
	if err != nil {
		c.JSON(500, gin.H{"error": "摘要保存失败，请重试"})
		return
	}
	if !saved {
		c.JSON(409, gin.H{"error": staleSummaryRetryMessage})
		return
	}
	settled = true
	jsonAfterCommit(c, 200, gin.H{"summary_brief": result.Brief, "summary_detailed": result.Detailed})
}
