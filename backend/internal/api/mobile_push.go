package api

import (
	"database/sql"
	"errors"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/bytedance/rss-pal/internal/mobilepush"
	"github.com/gin-gonic/gin"
)

type MobilePushHandler struct {
	keysDB *sql.DB
}

func NewMobilePushHandler(keysDB *sql.DB) *MobilePushHandler {
	return &MobilePushHandler{keysDB: keysDB}
}

func (h *MobilePushHandler) Status(c *gin.Context) {
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if !readPushBody(c, &input) || (input.Endpoint != "" && mobilepush.ValidateEndpoint(input.Endpoint) != nil) {
		invalidPushRequest(c)
		return
	}
	tx := c.MustGet(CtxKeyTx).(*sql.Tx)
	ctx := c.Request.Context()
	active := false
	if input.Endpoint != "" {
		var err error
		active, err = mobilepush.Enrolled(ctx, tx, getUserID(c), input.Endpoint)
		if err != nil {
			pushSettingsError(c)
			return
		}
	}
	keys, err := mobilepush.LoadKeys(ctx, h.keysDB)
	if err != nil {
		pushSettingsError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"active": active, "public_key": keys.Public})
}

func (h *MobilePushHandler) Enroll(c *gin.Context) {
	var input webpush.Subscription
	if !readPushBody(c, &input) || !mobilepush.ValidSubscription(input) {
		invalidPushRequest(c)
		return
	}
	tx := c.MustGet(CtxKeyTx).(*sql.Tx)
	if err := mobilepush.Enroll(c.Request.Context(), tx, getUserID(c), input); err != nil {
		if errors.Is(err, mobilepush.ErrEndpointOwned) {
			c.JSON(http.StatusConflict, gin.H{"error": "此设备已关联另一个账号"})
			return
		}
		pushSettingsError(c)
		return
	}
	jsonAfterCommit(c, http.StatusOK, gin.H{"active": true})
}

func (h *MobilePushHandler) Remove(c *gin.Context) {
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if !readPushBody(c, &input) || mobilepush.ValidateEndpoint(input.Endpoint) != nil {
		invalidPushRequest(c)
		return
	}
	tx := c.MustGet(CtxKeyTx).(*sql.Tx)
	if err := mobilepush.Remove(c.Request.Context(), tx, getUserID(c), input.Endpoint); err != nil {
		pushSettingsError(c)
		return
	}
	jsonAfterCommit(c, http.StatusOK, gin.H{"active": false})
}

func readPushBody(c *gin.Context, into any) bool {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	return c.ShouldBindJSON(into) == nil
}

func invalidPushRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "通知订阅信息无效"})
}

func pushSettingsError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "无法更新通知设置，请重试"})
}
