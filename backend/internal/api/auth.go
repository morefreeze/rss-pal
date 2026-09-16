package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/registrationpolicy"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/lib/pq"
)

type AuthHandler struct {
	registrationVerifier RegistrationVerifier
	registrationBudget   *AuthAbuseGuard
	cfg                  *config.Config
	userRepo             *repository.UserRepository
	refreshRepo          *repository.RefreshTokenRepository
}

func NewAuthHandler(cfg *config.Config, userRepo *repository.UserRepository, refreshRepo *repository.RefreshTokenRepository) *AuthHandler {
	var verifier RegistrationVerifier
	switch cfg.Auth.CaptchaProvider {
	case "tencent":
		appID, err := strconv.ParseUint(cfg.Auth.TencentCaptchaAppID, 10, 64)
		if err != nil {
			appID = 0
		}
		verifier = NewTencentCaptchaVerifier(appID, cfg.Auth.TencentCaptchaAppSecret, cfg.Auth.TencentSecretID, cfg.Auth.TencentSecretKey)
	case "turnstile", "":
		verifier = NewTurnstileVerifier(cfg.Auth.TurnstileSecret, cfg.Auth.TurnstileHostnames)
	}
	return &AuthHandler{cfg: cfg, userRepo: userRepo, refreshRepo: refreshRepo, registrationVerifier: verifier}
}

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

const (
	tokenTTL         = 7 * 24 * time.Hour
	tokenRenewBefore = 3 * 24 * time.Hour
	newTokenHeader   = "X-New-Token"
	// refreshTokenTTL is the lifetime of a "记住此设备" refresh token. Long
	// enough that an occasional-use device (every few weeks) never sees the
	// login prompt; short enough that an unused old device eventually requires
	// re-auth without the user explicitly revoking it.
	refreshTokenTTL = 90 * 24 * time.Hour
)

func (h *AuthHandler) signToken(userID int, username string, isAdmin bool) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.cfg.JWT.Secret))
}

func (h *AuthHandler) generateToken(user *model.User) (string, error) {
	return h.signToken(user.ID, user.Username, user.IsAdmin)
}

func (h *AuthHandler) InitAdmin(c *gin.Context) {
	exists, err := h.userRepo.WithCtx(c).AdminExists()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "admin already exists"})
		return
	}

	_, err = h.userRepo.WithCtx(c).CreateAdmin("admin", h.cfg.Auth.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userRepo := h.userRepo.WithCtx(c)
	user, err := userRepo.FindByUsername(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if user == nil || !userRepo.VerifyPassword(user, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	token, err := h.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{"token": token, "user": user}
	if req.Remember && h.refreshRepo != nil {
		ua := c.GetHeader("User-Agent")
		plaintext, _, rerr := h.refreshRepo.WithCtx(c).Issue(user.ID, refreshTokenTTL, ua)
		if rerr != nil {
			// Refresh-token failure shouldn't break login — fall back to the
			// regular access-only response so the user can still sign in.
			c.Header("X-Refresh-Issue-Error", "1")
		} else {
			resp["refresh_token"] = plaintext
			resp["refresh_token_expires_at"] = time.Now().Add(refreshTokenTTL).UTC()
		}
	}
	c.JSON(http.StatusOK, resp)
}

// Refresh trades a valid refresh token for a fresh access JWT. Unauthenticated
// endpoint — the refresh token itself is the credential. The refresh token
// stays the same (no rotation in v1) for the rest of its TTL.
func (h *AuthHandler) Refresh(c *gin.Context) {
	if h.refreshRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "refresh disabled"})
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing refresh_token"})
		return
	}

	hash := repository.HashRefreshToken(req.RefreshToken)
	row, err := h.refreshRepo.FindActiveByHash(hash)
	if err == repository.ErrInvalidRefreshToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token invalid"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userRepo.FindByID(row.UserID)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token invalid"})
		return
	}

	token, err := h.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Best-effort: bump last_used_at. A failure here doesn't break the refresh.
	_ = h.refreshRepo.TouchLastUsed(row.ID)

	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

// Logout revokes the supplied refresh token (if any) so a stolen-laptop
// scenario can be remediated. Stateless wrt the access JWT — the client is
// expected to forget it locally; the JWT itself remains valid until its TTL
// elapses since we have no JWT blacklist.
func (h *AuthHandler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req) // body is optional
	if req.RefreshToken != "" && h.refreshRepo != nil {
		hash := repository.HashRefreshToken(req.RefreshToken)
		_ = h.refreshRepo.RevokeByHash(hash)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "注册信息无效，请检查用户名、密码和邀请码"})
		return
	}

	if (req.Code == "") == (req.ShareRef == "") {
		c.JSON(400, gin.H{"error": "请提供邀请码或有效分享邀请"})
		return
	}
	if len(req.Password) > 72 {
		c.JSON(400, gin.H{"error": "密码不能超过 72 字节"})
		return
	}
	if h.registrationVerifier == nil {
		c.JSON(503, gin.H{"error": "注册验证暂时不可用，请稍后重试"})
		return
	}
	proof := req.CaptchaResponse
	if proof == "" && h.cfg.Auth.CaptchaProvider != "tencent" {
		proof = req.TurnstileResponse
	}
	if err := h.registrationVerifier.Verify(c.Request.Context(), proof, c.ClientIP()); err != nil {
		if errors.Is(err, ErrVerificationUnavailable) {
			c.JSON(503, gin.H{"error": "注册验证暂时不可用，请稍后重试"})
		} else {
			c.JSON(403, gin.H{"error": "请重新完成人机验证"})
		}
		return
	}
	if h.registrationBudget != nil && !h.registrationBudget.AdmitRegistration(c) {
		return
	}
	var user *model.User
	var err error
	if req.ShareRef != "" {
		source, sourceErr := h.registrationShareSource(req.ShareRef)
		if sourceErr != nil {
			err = sourceErr
		} else {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			user, err = h.userRepo.WithCtx(c).RegisterFromShare(ctx, req.Username, req.Password, source, registrationpolicy.Parse(h.cfg.Auth.ShareRegistrationMode, h.cfg.Auth.ShareRegistrationOwners))
		}
	} else {
		user, err = h.userRepo.WithCtx(c).Register(req.Username, req.Password, req.Code)
	}
	if errors.Is(err, repository.ErrInvalidRegistrationShare) {
		c.JSON(400, gin.H{"error": "分享邀请无效或已过期，请重新打开有效分享链接或使用邀请码"})
		return
	}
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" && pgErr.Constraint == "users_username_key" {
				c.JSON(http.StatusConflict, gin.H{"error": "用户名已被占用，请更换用户名"})
				return
			}
			// Never log PostgreSQL Detail or the request: they may contain credentials.
			log.Printf("registration database failure: sqlstate=%s constraint=%s", pgErr.Code, pgErr.Constraint)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "注册服务暂时不可用，请稍后重试"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "注册信息无效，请检查用户名、密码和邀请码"})
		return
	}

	token, err := h.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		tokenStr := authHeader
		if len(tokenStr) > 7 && tokenStr[:7] == "Bearer " {
			tokenStr = tokenStr[7:]
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(h.cfg.JWT.Secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// Sliding renewal: when the token has less than tokenRenewBefore
		// left, issue a fresh one in X-New-Token. Set before c.Next() so
		// streaming handlers don't race the header flush.
		if claims.ExpiresAt != nil && time.Until(claims.ExpiresAt.Time) < tokenRenewBefore {
			if fresh, err := h.signToken(claims.UserID, claims.Username, claims.IsAdmin); err == nil {
				c.Writer.Header().Set(newTokenHeader, fresh)
			}
		}

		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("isAdmin", claims.IsAdmin)
		c.Next()
	}
}

func (h *AuthHandler) GetMe(c *gin.Context) {
	userID := c.GetInt("userID")
	user, err := h.userRepo.WithCtx(c).FindByID(userID)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写旧密码和新密码"})
		return
	}
	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少 6 位"})
		return
	}

	userID := getUserID(c)
	userRepo := h.userRepo.WithCtx(c)
	user, err := userRepo.FindByID(userID)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	if !userRepo.VerifyPassword(user, req.OldPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "旧密码不正确"})
		return
	}
	if err := userRepo.ChangePassword(userID, req.NewPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "修改失败，请重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码已修改"})
}

func (h *AuthHandler) CreateInviteCode(c *gin.Context) {
	if !c.GetBool("isAdmin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}

	userRepo := h.userRepo.WithCtx(c)
	count, err := userRepo.CountNonAdminUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if count >= 10 {
		c.JSON(http.StatusForbidden, gin.H{"error": "已达到测试用户上限（10人）"})
		return
	}

	var req model.CreateInviteCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.ExpiresInHours = 72 // default 3 days
	}

	userID := c.GetInt("userID")
	code, err := userRepo.CreateInviteCode(userID, req.ExpiresInHours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, code)
}

func (h *AuthHandler) ListInviteCodes(c *gin.Context) {
	if !c.GetBool("isAdmin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}

	codes, err := h.userRepo.WithCtx(c).ListInviteCodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, codes)
}

// UpdateVisibilityFloor lets the caller move their own shared-content floor.
// Body: {"days_back": N}. N must be >= 0. Larger N = more history. New users
// default to 7. Owner-owned (private) feeds are unaffected — the floor only
// gates shared (owner_id IS NULL) feeds.
func (h *AuthHandler) UpdateVisibilityFloor(c *gin.Context) {
	var req struct {
		DaysBack int `json:"days_back" binding:"min=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days_back must be a non-negative integer"})
		return
	}
	floor, err := h.userRepo.WithCtx(c).UpdateSharedVisibleFrom(getUserID(c), req.DaysBack)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"shared_visible_from": floor,
		"days_back":           req.DaysBack,
	})
}

func getUserID(c *gin.Context) int {
	return c.GetInt("userID")
}

func getOwnerID(c *gin.Context) *int {
	id := c.GetInt("userID")
	return &id
}

func isAdmin(c *gin.Context) bool {
	return c.GetBool("isAdmin")
}

func getIntParam(c *gin.Context, param string) (int, bool) {
	id, err := strconv.Atoi(c.Param(param))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + param})
		return 0, false
	}
	return id, true
}

// RegistrationConfig exposes public provider configuration only.
func (h *AuthHandler) RegistrationConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	provider := h.cfg.Auth.CaptchaProvider
	if provider == "" {
		provider = "turnstile"
	}
	available, siteKey := false, ""
	switch provider {
	case "tencent":
		appID, err := strconv.ParseUint(h.cfg.Auth.TencentCaptchaAppID, 10, 64)
		available = err == nil && appID > 0 && strings.TrimSpace(h.cfg.Auth.TencentCaptchaAppSecret) != "" && strings.TrimSpace(h.cfg.Auth.TencentSecretID) != "" && strings.TrimSpace(h.cfg.Auth.TencentSecretKey) != ""
		siteKey = h.cfg.Auth.TencentCaptchaAppID
	case "turnstile":
		available = h.cfg.Auth.TurnstileSiteKey != "" && h.cfg.Auth.TurnstileSecret != "" && len(h.cfg.Auth.TurnstileHostnames) > 0
		siteKey = h.cfg.Auth.TurnstileSiteKey
	}
	c.JSON(200, gin.H{"available": available, "provider": provider, "site_key": strings.TrimSpace(siteKey)})
}

func (h *AuthHandler) SetRegistrationBudget(guard *AuthAbuseGuard) { h.registrationBudget = guard }
