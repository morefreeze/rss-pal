package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	EnglifeSessionKey string
	Server            ServerConfig
	Database          DatabaseConfig
	Claude            ClaudeConfig
	AI                AIConfig
	Auth              AuthConfig
	JWT               JWTConfig
	RSSHub            RSSHubConfig
	Backup            BackupConfig
	Explore           ExploreConfig
	Share             ShareConfig
}

type BackupConfig struct {
	Dir string // host-mounted; survives container removal
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string

	// AdminUser/AdminPassword are used by the backup/restore admin handler
	// and the BackupRunner. They must be a Postgres role with privilege to
	// read all users' rows and write into RLS-protected tables (typically
	// a SUPERUSER like postgres, or any role with BYPASSRLS). Defaults:
	// fall back to User/Password so dev/test setups that don't distinguish
	// runtime from admin keep working.
	AdminUser     string
	AdminPassword string
}

type ClaudeConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type AIConfig struct {
	Vision VisionConfig
}

// VisionConfig groups everything the vision-summary path needs.
// Defaults are tuned for z.ai's glm-5v-turbo, 6-image cap, 1024 longest-side,
// 4 MB base64 payload budget, 24h cache TTL.
type VisionConfig struct {
	Model           string        // chat completions "model" field for vision calls
	MaxImages       int           // hard cap per article
	MaxLongSide     int           // resize threshold; px
	PayloadBudgetMB int           // base64 budget; drops tail images on overflow
	MinImages       int           // auto-trigger image-count floor
	MaxTextChars    int           // auto-trigger text-length ceiling
	CacheDir        string        // temp cache root
	CacheTTL        time.Duration // cache file age limit
}

type AuthConfig struct {
	PublicRegistration bool
	PublicOrigin       string
	OTPSecret          string
	EmailProvider      string
	SESSecretID        string
	SESSecretKey       string
	SESRegion          string
	SESTemplateID      string
	SESFrom            string
	SESReplyTo         string
	SMTPHost           string
	SMTPPort           string
	SMTPUsername       string
	SMTPPassword       string
	SMTPFrom           string
	GitHubClientID     string
	GitHubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string
	AppleClientID      string
	AppleTeamID        string
	AppleKeyID         string
	ApplePrivateKey    string

	CaptchaProvider         string
	TencentCaptchaAppID     string
	TencentCaptchaAppSecret string
	TencentSecretID         string
	TencentSecretKey        string
	ShareRegistrationMode   string
	ShareRegistrationOwners string
	TurnstileSiteKey        string
	TurnstileSecret         string
	TurnstileHostnames      []string
	Password                string
}

type JWTConfig struct {
	Secret string
}

type RSSHubConfig struct {
	BaseURL string
}

// ExploreConfig controls bounded dispatcher work for subscription discovery.
type ExploreConfig struct {
	FetchBatchLimit  int
	FetchConcurrency int
}

type ShareConfig struct {
	Secret      string
	ShortOrigin string
}

const shortShareOrigin = "https://r.morefreeze.top"

func Load() *Config {
	return &Config{
		EnglifeSessionKey: getEnv("ENGLIFE_SESSION_KEY", ""),
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Database: DatabaseConfig{
			Host:          getEnv("DB_HOST", "localhost"),
			Port:          getEnv("DB_PORT", "5432"),
			User:          getEnv("DB_USER", "postgres"),
			Password:      getEnv("DB_PASSWORD", "postgres"),
			DBName:        getEnv("DB_NAME", "rsspal"),
			SSLMode:       getEnv("DB_SSLMODE", "disable"),
			AdminUser:     getEnv("DB_ADMIN_USER", getEnv("DB_USER", "postgres")),
			AdminPassword: getEnv("DB_ADMIN_PASSWORD", getEnv("DB_PASSWORD", "postgres")),
		},
		Claude: ClaudeConfig{
			APIKey:  getEnv("CLAUDE_API_KEY", ""),
			BaseURL: getEnv("CLAUDE_BASE_URL", "https://api.anthropic.com"),
			Model:   getEnv("CLAUDE_MODEL", "glm-5.3"),
		},
		AI: AIConfig{
			Vision: VisionConfig{
				Model:           getEnv("AI_VISION_MODEL", "glm-5v-turbo"),
				MaxImages:       getEnvInt("AI_VISION_MAX_IMAGES", 6),
				MaxLongSide:     getEnvInt("AI_VISION_MAX_LONG_SIDE", 1024),
				PayloadBudgetMB: getEnvInt("AI_VISION_PAYLOAD_BUDGET_MB", 4),
				MinImages:       getEnvInt("AI_VISION_MIN_IMAGES", 3),
				MaxTextChars:    getEnvInt("AI_VISION_MAX_TEXT_CHARS", 2000),
				CacheDir:        getEnv("AI_VISION_CACHE_DIR", "/backups/ai_summary_cache"),
				CacheTTL:        time.Duration(getEnvInt("AI_VISION_CACHE_TTL_HOURS", 24)) * time.Hour,
			},
		},
		Auth: AuthConfig{
			PublicRegistration:      getEnv("AUTH_PUBLIC_REGISTRATION", "false") == "true",
			PublicOrigin:            getEnv("AUTH_PUBLIC_ORIGIN", ""),
			OTPSecret:               getEnv("AUTH_OTP_SECRET", ""),
			EmailProvider:           getEnv("AUTH_EMAIL_PROVIDER", "smtp"),
			SESSecretID:             getEnv("TENCENT_SES_SECRET_ID", ""),
			SESSecretKey:            getEnv("TENCENT_SES_SECRET_KEY", ""),
			SESRegion:               getEnv("TENCENT_SES_REGION", "ap-hongkong"),
			SESTemplateID:           getEnv("TENCENT_SES_TEMPLATE_ID", ""),
			SESFrom:                 getEnv("TENCENT_SES_FROM", ""),
			SESReplyTo:              getEnv("TENCENT_SES_REPLY_TO", ""),
			SMTPHost:                getEnv("SMTP_HOST", ""),
			SMTPPort:                getEnv("SMTP_PORT", "587"),
			SMTPUsername:            getEnv("SMTP_USERNAME", ""),
			SMTPPassword:            getEnv("SMTP_PASSWORD", ""),
			SMTPFrom:                getEnv("SMTP_FROM", ""),
			GitHubClientID:          getEnv("GITHUB_CLIENT_ID", ""),
			GitHubClientSecret:      getEnv("GITHUB_CLIENT_SECRET", ""),
			GoogleClientID:          getEnv("GOOGLE_CLIENT_ID", ""),
			GoogleClientSecret:      getEnv("GOOGLE_CLIENT_SECRET", ""),
			AppleClientID:           getEnv("APPLE_CLIENT_ID", ""),
			AppleTeamID:             getEnv("APPLE_TEAM_ID", ""),
			AppleKeyID:              getEnv("APPLE_KEY_ID", ""),
			ApplePrivateKey:         getEnv("APPLE_PRIVATE_KEY", ""),
			CaptchaProvider:         getEnv("AUTH_CAPTCHA_PROVIDER", "tencent"),
			TencentCaptchaAppID:     getEnv("TENCENT_CAPTCHA_APP_ID", ""),
			TencentCaptchaAppSecret: getEnv("TENCENT_CAPTCHA_APP_SECRET", ""),
			TencentSecretID:         getEnv("TENCENT_CAPTCHA_SECRET_ID", ""),
			TencentSecretKey:        getEnv("TENCENT_CAPTCHA_SECRET_KEY", ""),
			ShareRegistrationMode:   getEnv("SHARE_REGISTRATION_MODE", "selected_owners"),
			ShareRegistrationOwners: getEnv("SHARE_REGISTRATION_OWNER_IDS", ""),
			Password:                getEnv("AUTH_PASSWORD", "admin"),
			TurnstileSiteKey:        getEnv("TURNSTILE_SITE_KEY", ""),
			TurnstileSecret:         getEnv("TURNSTILE_SECRET", ""),
			TurnstileHostnames:      strings.FieldsFunc(getEnv("TURNSTILE_HOSTNAMES", ""), func(r rune) bool { return r == ',' || r == ' ' }),
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "rss-pal-default-secret-change-me"),
		},
		RSSHub: RSSHubConfig{
			BaseURL: getEnv("RSSHUB_BASE_URL", "http://rsshub:1200"),
		},
		Backup: BackupConfig{
			Dir: getEnv("BACKUP_DIR", "/backups"),
		},
		Explore: ExploreConfig{
			FetchBatchLimit:  getEnvIntBounded("EXPLORE_FETCH_BATCH_LIMIT", 500, 1, 500),
			FetchConcurrency: getEnvIntBounded("EXPLORE_FETCH_CONCURRENCY", 5, 1, 5),
		},
		Share: ShareConfig{
			Secret:      getEnv("SHARE_SECRET", ""),
			ShortOrigin: getEnv("SHORT_SHARE_ORIGIN", ""),
		},
	}
}

func ValidateShortShareOrigin(raw string) (string, error) {
	if raw != shortShareOrigin {
		return "", errors.New("SHORT_SHARE_ORIGIN must be exactly https://r.morefreeze.top")
	}
	return shortShareOrigin, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

// getEnvIntBounded returns defaultValue unless key contains an integer at
// least minValue and, when maxValue is positive, no greater than maxValue.
func getEnvIntBounded(key string, defaultValue, minValue, maxValue int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n < minValue || (maxValue > 0 && n > maxValue) {
		return defaultValue
	}
	return n
}
