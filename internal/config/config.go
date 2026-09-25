package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds application configuration loaded from the environment.
type Config struct {
	AppEnv        string
	APIPort       string
	DatabaseURL   string
	JWTSecret     string
	FrontendURL   string // primary origin (first FRONTEND_URL) for redirects
	CORSOrigins   string // comma-separated allowlist for CORS
	StorageRoot   string
	StorageDriver string
	// PublicAPIURL is used for Twilio status callbacks (must be reachable by Twilio).
	PublicAPIURL       string
	GoogleClientID     string
	GoogleClientSecret string
	GmailPubSubTopic   string
	TokenEncryptionKey string
}

// Load reads configuration from environment variables.
// It optionally loads a .env file when present (local development).
func Load() (*Config, error) {
	// A missing .env is expected in deployed environments, but one that exists
	// and fails to parse must fail loudly: godotenv loads nothing on error, which
	// otherwise surfaces as a misleading "missing required environment variables".
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("parse .env: %w", err)
	}

	frontendRaw := getEnv("FRONTEND_URL", "http://localhost:3000")
	cfg := &Config{
		AppEnv:             getEnv("APP_ENV", "development"),
		APIPort:            getEnv("API_PORT", getEnv("PORT", "8080")),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		FrontendURL:        primaryFrontendURL(frontendRaw),
		CORSOrigins:        frontendRaw,
		StorageRoot:        getEnv("STORAGE_ROOT", "./storage"),
		StorageDriver:      getEnv("STORAGE_DRIVER", "local"),
		PublicAPIURL:       getEnv("PUBLIC_API_URL", "http://localhost:8080"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GmailPubSubTopic:   os.Getenv("GMAIL_PUBSUB_TOPIC"),
		TokenEncryptionKey: getEnv("TOKEN_ENCRYPTION_KEY", os.Getenv("JWT_SECRET")),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string

	if strings.TrimSpace(c.DatabaseURL) == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if strings.TrimSpace(c.APIPort) == "" {
		missing = append(missing, "API_PORT")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if _, err := strconv.Atoi(c.APIPort); err != nil {
		return fmt.Errorf("API_PORT must be a valid port number: %w", err)
	}

	return nil
}

// IsDevelopment reports whether the app is running in development mode.
func (c *Config) IsDevelopment() bool {
	return strings.EqualFold(c.AppEnv, "development") || strings.EqualFold(c.AppEnv, "dev")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// primaryFrontendURL returns the first origin from a comma-separated FRONTEND_URL list.
func primaryFrontendURL(raw string) string {
	for _, part := range strings.Split(raw, ",") {
		if o := strings.TrimRight(strings.TrimSpace(part), "/"); o != "" {
			return o
		}
	}
	return "http://localhost:3000"
}
