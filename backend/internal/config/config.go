package config

import (
	"os"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Claude   ClaudeConfig
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
}

type ClaudeConfig struct {
	APIKey  string
	BaseURL string
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
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "rsspal"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Claude: ClaudeConfig{
			APIKey:  getEnv("CLAUDE_API_KEY", ""),
			BaseURL: getEnv("CLAUDE_BASE_URL", "https://api.anthropic.com"),
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
