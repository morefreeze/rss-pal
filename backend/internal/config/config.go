package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Claude   ClaudeConfig
	AI       AIConfig
	Auth     AuthConfig
	JWT      JWTConfig
	RSSHub   RSSHubConfig
	Backup   BackupConfig
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
}

type AIConfig struct {
	Vision VisionConfig
}

// VisionConfig groups everything the vision-summary path needs.
// Defaults are tuned for z.ai's glm-4v-plus, 6-image cap, 1024 longest-side,
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
	Password string
}

type JWTConfig struct {
	Secret string
}

type RSSHubConfig struct {
	BaseURL string
}

func Load() *Config {
	return &Config{
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
		},
		AI: AIConfig{
			Vision: VisionConfig{
				Model:           getEnv("AI_VISION_MODEL", "glm-4v-plus"),
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
			Password: getEnv("AUTH_PASSWORD", "admin"),
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
	}
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
