package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shares   *repository.ShareRepository
	articles *repository.ArticleRepository
	signer   *sharetoken.Signer
	now      func() time.Time
	images   *ArticleImageHandler
}

func NewShareHandler(
	shares *repository.ShareRepository,
	articles *repository.ArticleRepository,
	signer *sharetoken.Signer,
	images *ArticleImageHandler,
	now func() time.Time,
) *ShareHandler {
	if now == nil {
		now = time.Now
	}
	return &ShareHandler{shares: shares, articles: articles, signer: signer, now: now, images: images}
}

type shareManagementResponse struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
	Legacy    bool       `json:"legacy"`
}

func (h *ShareHandler) GetPublic(c *gin.Context) {
	setPublicShareHeaders(c)
	row, ok := h.resolveActive(c.Param("token"))
	if !ok {
		shareUnavailable(c)
		return
	}

	snapshot := row.Snapshot
	snapshot.Content = rewriteShareAssets(snapshot.Content, c.Param("token"), row.ArticleID)
	c.JSON(http.StatusOK, snapshot)
}

func (h *ShareHandler) GetAsset(c *gin.Context) {
	setPublicShareHeaders(c)
	row, ok := h.resolveActive(c.Param("token"))
	if !ok {
		shareUnavailable(c)
		return
	}
	if h.images == nil {
		log.Printf("share asset failed: image handler unavailable")
		shareUnavailable(c)
		return
	}
	h.images.serve(c, row.ArticleID, c.Param("asset"), "no-store")
}

func (h *ShareHandler) resolveActive(token string) (*model.ArticleShare, bool) {
	value, legacy, err := h.signer.Parse(token)
	if err != nil {
		return nil, false
	}
	var row *model.ArticleShare
	if legacy {
		row, err = h.shares.GetActiveByLegacyDigest(sharetoken.LegacyDigest(value), h.now())
	} else {
		row, err = h.shares.GetActiveByPublicID(value, h.now())
	}
	if err != nil {
		log.Printf("share public resolve failed: %v", err)
		return nil, false
	}
	return row, row != nil
}

var (
	strictMarkdownImagePattern = regexp.MustCompile(`!\[[^\]\r\n]*\]\(/api/articles/[0-9]+/images/[0-9]+\.(png|jpg|jpeg)\)`)
	htmlTagPattern             = regexp.MustCompile(`(?i)<[a-z][^<>]*>`)
)

func rewriteShareAssets(content, token string, articleID int) string {
	// Deliberately avoid substring replacement: putting a signed share URL into
	// an attacker-controlled external URL would disclose the token when loaded.
	// Only image destinations/attributes are inspected, and localShareAsset
	// requires their complete value to name this share's article.
	assetPrefix := "/api/share/" + url.PathEscape(token) + "/assets/"
	return rewriteOutsideFencedCode(content, articleID, assetPrefix)
}

func rewriteOutsideFencedCode(content string, articleID int, assetPrefix string) string {
	var result strings.Builder
	result.Grow(len(content))
	normalStart := 0
	for i := 0; i < len(content); i++ {
		if end, ok := fencedCodeEnd(content, i); ok {
			result.WriteString(rewriteOutsideInlineCode(content[normalStart:i], articleID, assetPrefix))
			result.WriteString(content[i:end])
			i = end - 1
			normalStart = end
		}
	}
	result.WriteString(rewriteOutsideInlineCode(content[normalStart:], articleID, assetPrefix))
	return result.String()
}

type backtickRun struct {
	start  int
	length int
}

func rewriteOutsideInlineCode(content string, articleID int, assetPrefix string) string {
	runs := make([]backtickRun, 0)
	for i := 0; i < len(content); {
		if content[i] != '`' {
			i++
			continue
		}
		length := byteRun(content, i, '`')
		runs = append(runs, backtickRun{start: i, length: length})
		i += length
	}
	if len(runs) == 0 {
		return rewriteNonCode(content, articleID, assetPrefix)
	}

	nextSameLength := make([]int, len(runs))
	lastByLength := make(map[int]int)
	for i := len(runs) - 1; i >= 0; i-- {
		nextSameLength[i] = -1
		if next, ok := lastByLength[runs[i].length]; ok {
			nextSameLength[i] = next
		}
		lastByLength[runs[i].length] = i
	}

	var result strings.Builder
	result.Grow(len(content))
	normalStart := 0
	for i := 0; i < len(runs); {
		opening := runs[i]
		if opening.start < normalStart {
			i++
			continue
		}
		closingIndex := nextSameLength[i]
		if closingIndex < 0 {
			// An unmatched run is ordinary text. Leave it in the non-code
			// segment so valid images later in the document are still handled.
			i++
			continue
		}
		closing := runs[closingIndex]
		result.WriteString(rewriteNonCode(content[normalStart:opening.start], articleID, assetPrefix))
		codeEnd := closing.start + closing.length
		result.WriteString(content[opening.start:codeEnd])
		normalStart = codeEnd
		i = closingIndex + 1
	}
	result.WriteString(rewriteNonCode(content[normalStart:], articleID, assetPrefix))
	return result.String()
}

func rewriteNonCode(content string, articleID int, assetPrefix string) string {
	tags := htmlTagPattern.FindAllStringIndex(content, -1)
	if len(tags) == 0 {
		return rewriteMarkdownImages(content, articleID, assetPrefix)
	}
	var result strings.Builder
	result.Grow(len(content))
	last := 0
	for _, tagRange := range tags {
		result.WriteString(rewriteMarkdownImages(content[last:tagRange[0]], articleID, assetPrefix))
		tag := content[tagRange[0]:tagRange[1]]
		if isHTMLImageTag(tag) {
			result.WriteString(rewriteHTMLImageTag(tag, articleID, assetPrefix))
		} else {
			result.WriteString(tag)
		}
		last = tagRange[1]
	}
	result.WriteString(rewriteMarkdownImages(content[last:], articleID, assetPrefix))
	return result.String()
}

func rewriteMarkdownImages(content string, articleID int, assetPrefix string) string {
	matches := strictMarkdownImagePattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content
	}
	var result strings.Builder
	result.Grow(len(content))
	last := 0
	for _, matchRange := range matches {
		match := content[matchRange[0]:matchRange[1]]
		destinationStart := strings.Index(match, "](") + 2
		destinationEnd := len(match) - 1
		asset, ok := localShareAsset(match[destinationStart:destinationEnd], articleID)
		if !ok {
			continue
		}
		result.WriteString(content[last : matchRange[0]+destinationStart])
		result.WriteString(assetPrefix)
		result.WriteString(asset)
		last = matchRange[0] + destinationEnd
	}
	if last == 0 {
		return content
	}
	result.WriteString(content[last:])
	return result.String()
}

func fencedCodeEnd(content string, start int) (int, bool) {
	if start > 0 && content[start-1] != '\n' {
		return 0, false
	}
	i := start
	for spaces := 0; spaces < 3 && i < len(content) && content[i] == ' '; spaces++ {
		i++
	}
	if i >= len(content) || (content[i] != '`' && content[i] != '~') {
		return 0, false
	}
	fence := content[i]
	run := byteRun(content, i, fence)
	if run < 3 {
		return 0, false
	}
	lineEnd := strings.IndexByte(content[i+run:], '\n')
	if lineEnd < 0 {
		return len(content), true
	}
	lineStart := i + run + lineEnd + 1
	for lineStart < len(content) {
		candidate := lineStart
		for spaces := 0; spaces < 3 && candidate < len(content) && content[candidate] == ' '; spaces++ {
			candidate++
		}
		closingRun := byteRun(content, candidate, fence)
		candidateEnd := candidate + closingRun
		nextLine := strings.IndexByte(content[lineStart:], '\n')
		end := len(content)
		if nextLine >= 0 {
			end = lineStart + nextLine + 1
		}
		lineContentEnd := end
		if lineContentEnd > lineStart && content[lineContentEnd-1] == '\n' {
			lineContentEnd--
		}
		if lineContentEnd > lineStart && content[lineContentEnd-1] == '\r' {
			lineContentEnd--
		}
		if closingRun >= run && strings.Trim(content[candidateEnd:lineContentEnd], " \t") == "" {
			return end, true
		}
		lineStart = end
	}
	return len(content), true
}

func byteRun(content string, start int, value byte) int {
	end := start
	for end < len(content) && content[end] == value {
		end++
	}
	return end - start
}

func isHTMLImageTag(tag string) bool {
	if len(tag) < 5 || !strings.EqualFold(tag[1:4], "img") {
		return false
	}
	return tag[4] == '>' || tag[4] == '/' || isHTMLSpace(tag[4])
}

func rewriteHTMLImageTag(tag string, articleID int, assetPrefix string) string {
	var result strings.Builder
	last := 0
	for i := 4; i < len(tag)-1; {
		for i < len(tag)-1 && isHTMLSpace(tag[i]) {
			i++
		}
		nameStart := i
		for i < len(tag)-1 && isHTMLAttributeNameByte(tag[i]) {
			i++
		}
		if nameStart == i {
			i++
			continue
		}
		name := tag[nameStart:i]
		for i < len(tag)-1 && isHTMLSpace(tag[i]) {
			i++
		}
		if i >= len(tag)-1 || tag[i] != '=' {
			continue
		}
		i++
		for i < len(tag)-1 && isHTMLSpace(tag[i]) {
			i++
		}
		valueStart, valueEnd := i, i
		if i < len(tag)-1 && (tag[i] == '\'' || tag[i] == '"') {
			quote := tag[i]
			valueStart = i + 1
			valueEnd = valueStart
			for valueEnd < len(tag)-1 && tag[valueEnd] != quote {
				valueEnd++
			}
			i = valueEnd
			if i < len(tag)-1 {
				i++
			}
		} else {
			for valueEnd < len(tag)-1 && !isHTMLSpace(tag[valueEnd]) && tag[valueEnd] != '>' {
				valueEnd++
			}
			i = valueEnd
		}
		if strings.EqualFold(name, "src") {
			if asset, ok := localShareAsset(tag[valueStart:valueEnd], articleID); ok {
				result.WriteString(tag[last:valueStart])
				result.WriteString(assetPrefix)
				result.WriteString(asset)
				last = valueEnd
			}
		}
	}
	if last == 0 {
		return tag
	}
	result.WriteString(tag[last:])
	return result.String()
}

func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func isHTMLAttributeNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == ':' || b == '-' || b == '_'
}

func localShareAsset(value string, articleID int) (string, bool) {
	prefix := "/api/articles/" + strconv.Itoa(articleID) + "/images/"
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	asset := value[len(prefix):]
	dot := strings.LastIndexByte(asset, '.')
	if dot <= 0 || dot == len(asset)-1 {
		return "", false
	}
	for i := 0; i < dot; i++ {
		if asset[i] < '0' || asset[i] > '9' {
			return "", false
		}
	}
	switch asset[dot+1:] {
	case "png", "jpg", "jpeg":
		return asset, true
	default:
		return "", false
	}
}

func setPublicShareHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
}

func shareUnavailable(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "share unavailable"})
}

func (h *ShareHandler) Create(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}

	now := h.now()
	expiresAt, ok := parseShareExpiry(c, now)
	if !ok {
		return
	}

	userID := getUserID(c)
	article, ok := h.visibleArticle(c, articleID, userID)
	if !ok {
		return
	}
	if (article.ProcessingState != "" && article.ProcessingState != "ready") || strings.TrimSpace(article.Content) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "article is not ready to share"})
		return
	}

	publicID, err := sharetoken.NewPublicID()
	if err != nil {
		shareInternalError(c, "generate public ID", err)
		return
	}
	row, err := h.shares.WithCtx(c).Create(article, userID, publicID, expiresAt, now)
	if err != nil {
		shareInternalError(c, "create", err)
		return
	}
	c.JSON(http.StatusCreated, h.managementResponse(row, now))
}

func (h *ShareHandler) List(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}
	userID := getUserID(c)
	if _, ok := h.visibleArticle(c, articleID, userID); !ok {
		return
	}

	now := h.now()
	rows, err := h.shares.WithCtx(c).List(articleID, userID, now)
	if err != nil {
		shareInternalError(c, "list", err)
		return
	}
	response := make([]shareManagementResponse, 0, len(rows))
	for i := range rows {
		response = append(response, h.managementResponse(&rows[i], now))
	}
	c.JSON(http.StatusOK, response)
}

func (h *ShareHandler) Revoke(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}
	userID := getUserID(c)
	if _, ok := h.visibleArticle(c, articleID, userID); !ok {
		return
	}
	if !validSharePublicID(c.Param("share_id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}

	now := h.now()
	row, err := h.shares.WithCtx(c).Revoke(c.Param("share_id"), articleID, userID, now)
	if err != nil {
		shareInternalError(c, "revoke", err)
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	c.JSON(http.StatusOK, h.managementResponse(row, now))
}

func (h *ShareHandler) visibleArticle(c *gin.Context, articleID, userID int) (*model.Article, bool) {
	article, err := h.articles.WithCtx(c).GetByID(articleID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return nil, false
	}
	if err != nil {
		shareInternalError(c, "authorize article", err)
		return nil, false
	}
	return article, true
}

func (h *ShareHandler) managementResponse(row *model.ArticleShare, now time.Time) shareManagementResponse {
	status := "active"
	if row.RevokedAt != nil {
		status = "revoked"
	} else if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
		status = "expired"
	}
	return shareManagementResponse{
		ID:        row.PublicID,
		URL:       "/share/" + h.signer.Sign(row.PublicID),
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
		Status:    status,
		Legacy:    row.LegacyTokenDigest != nil,
	}
}

func parseShareArticleID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func parseShareExpiry(c *gin.Context, now time.Time) (*time.Time, bool) {
	var request struct {
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return nil, false
	}

	raw := bytes.TrimSpace(request.ExpiresAt)
	if len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at is required"})
		return nil, false
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be null or a future RFC3339 timestamp"})
		return nil, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || !parsed.After(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be null or a future RFC3339 timestamp"})
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func validSharePublicID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !((id[i] >= '0' && id[i] <= '9') || (id[i] >= 'a' && id[i] <= 'f')) {
			return false
		}
	}
	return true
}

func shareInternalError(c *gin.Context, operation string, err error) {
	log.Printf("share %s failed: %v", operation, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
