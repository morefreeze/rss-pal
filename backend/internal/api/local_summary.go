package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
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
	templateID := 0
	if raw := c.Query("template_id"); raw != "" {
		templateID, err = strconv.Atoi(raw)
		if err != nil || templateID <= 0 {
			c.JSON(400, gin.H{"error": "invalid template"})
			return
		}
	}
	briefTemplate, detailedTemplate, status := h.localSummaryTemplates(c, templateID)
	if status != 0 {
		c.JSON(status, gin.H{"error": "template not found"})
		return
	}
	brief, detailed := ai.LocalSummaryPrompts(article.Title, article.Content, briefTemplate, detailedTemplate)
	generation := ai.LocalSummaryGeneration(article.Title, article.Content, briefTemplate, detailedTemplate, templateID)
	c.JSON(200, gin.H{"content_hash": localSummaryHash(article.Content), "summary_version": localSummaryHash(article.SummaryBrief + "\x00" + article.SummaryDetailed), "brief_prompt": brief, "detailed_prompt": detailed, "generation_binding": generation.BindingHash, "template_id": templateID})

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
		ContentHash       string   `json:"content_hash"`
		SummaryVersion    string   `json:"summary_version"`
		GenerationBinding string   `json:"generation_binding"`
		TemplateID        int      `json:"template_id"`
		Model             string   `json:"model"`
		RequestHashes     []string `json:"request_hashes"`
		Brief             string   `json:"summary_brief"`
		Detailed          string   `json:"summary_detailed"`
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
	if len(result.RequestHashes) != 2 || !localSummaryRequestHashRE.MatchString(result.RequestHashes[0]) || !localSummaryRequestHashRE.MatchString(result.RequestHashes[1]) {
		c.JSON(400, gin.H{"error": "缺少有效的请求指纹，请刷新后重新生成"})
		return
	}
	if !localSummaryModelRE.MatchString(result.Model) {
		c.JSON(400, gin.H{"error": "请输入有效的模型名称"})
		return
	}
	briefTemplate, detailedTemplate, status := h.localSummaryTemplates(c, result.TemplateID)
	if status != 0 {
		c.JSON(status, gin.H{"error": "template not found"})
		return
	}
	generation := ai.LocalSummaryGeneration(article.Title, article.Content, briefTemplate, detailedTemplate, result.TemplateID)
	if result.GenerationBinding != generation.BindingHash {
		c.JSON(409, gin.H{"error": "总结输入或规则已更新，请刷新文章后重新生成"})
		return
	}
	briefPrompt, detailedPrompt := ai.LocalSummaryPrompts(article.Title, article.Content, briefTemplate, detailedTemplate)
	ai.CompleteLocalSummaryGeneration(generation, result.Model, briefPrompt, detailedPrompt, result.Brief, result.RequestHashes)
	generation.BindTemplate(result.TemplateID, briefTemplate, detailedTemplate)

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
	saved, err := repo.CompleteLocalSummary(id, token, article.Content, result.Brief, result.Detailed, article.SummaryBrief, article.SummaryDetailed, generation)
	if err != nil {
		c.JSON(500, gin.H{"error": "摘要保存失败，请重试"})
		return
	}
	if !saved {
		c.JSON(409, gin.H{"error": staleSummaryRetryMessage})
		return
	}
	settled = true
	jsonAfterCommit(c, 200, gin.H{"summary_brief": result.Brief, "summary_detailed": result.Detailed, "summary_generation": generation})
}

var localSummaryRequestHashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var localSummaryModelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}$`)

func (h *ArticleHandler) localSummaryTemplates(c *gin.Context, id int) (string, string, int) {
	if id == 0 {
		return "", "", 0
	}
	if id < 0 || h.templateRepo == nil {
		return "", "", 400
	}
	tpl, err := h.templateRepo.WithCtx(c).GetByID(id)
	if err != nil || tpl == nil || (!tpl.IsSystem && (tpl.UserID == nil || *tpl.UserID != getUserID(c))) {
		return "", "", 404
	}
	return tpl.BriefPrompt, tpl.DetailedPrompt, 0
}
