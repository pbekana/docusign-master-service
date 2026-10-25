// Package config loads and validates service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all validated configuration for the service.
type Config struct {
	// Application
	AppEnv   string
	Port     string
	LogLevel string

	// Database
	DatabaseURL string
	DBMaxConns  int32
	DBMinConns  int32

	// DocuSign OAuth / JWT Grant
	DocusignIntegrationKey         string
	DocusignUserID                 string
	DocusignPrivateKeyPath         string
	DocusignAuthServer             string
	DocusignOAuthScopes            string
	DocusignAccountID              string
	DocusignAPIBaseURI             string
	DocusignRequestTimeoutSeconds  int
	DocusignPageSize               int

	// Rate limiting
	RateLimitMaxWaitSeconds      int
	RateLimitFallbackDelaySeconds int

	// Retry policy
	RetryMaxAttempts  int
	RetryBaseDelayMS  int
	RetryMaxDelayMS   int

	// MinIO
	MinioEndpoint   string
	MinioAccessKey  string
	MinioSecretKey  string
	MinioBucketName string
	MinioUseSSL     bool

	// HMAC request signing
	HMACSecretKeyCore             string
	HMACTimestampToleranceSeconds int64

	// Job worker
	MaxConcurrentJobs              int
	MaxConcurrentDocusignRequests  int
	HeartbeatIntervalSeconds       int
	CrashTimeoutMinutes            int

	// Local output
	LocalOutputDir string
}

// requiredVars lists env vars that must be non-empty.
var requiredVars = []string{
	"DATABASE_URL",
	"DOCUSIGN_INTEGRATION_KEY",
	"DOCUSIGN_USER_ID",
	"DOCUSIGN_PRIVATE_KEY_PATH",
	"DOCUSIGN_AUTH_SERVER",
	"MINIO_ENDPOINT",
	"MINIO_ACCESS_KEY",
	"MINIO_SECRET_KEY",
	"HMAC_SECRET_KEY_CORE",
}

// LoadConfig reads environment variables, applies defaults, and validates that
// all required variables are present. It returns a Config or an error that
// lists every missing variable.
func LoadConfig() (*Config, error) {
	var missing []string
	for _, v := range requiredVars {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	cfg := &Config{
		// Application
		AppEnv:   envOrDefault("APP_ENV", "development"),
		Port:     envOrDefault("PORT", "8080"),
		LogLevel: envOrDefault("LOG_LEVEL", "info"),

		// Database
		DatabaseURL: os.Getenv("DATABASE_URL"),
		DBMaxConns:  int32(envIntOrDefault("DB_MAX_CONNS", 10)),
		DBMinConns:  int32(envIntOrDefault("DB_MIN_CONNS", 2)),

		// DocuSign
		DocusignIntegrationKey:        os.Getenv("DOCUSIGN_INTEGRATION_KEY"),
		DocusignUserID:                os.Getenv("DOCUSIGN_USER_ID"),
		DocusignPrivateKeyPath:        os.Getenv("DOCUSIGN_PRIVATE_KEY_PATH"),
		DocusignAuthServer:            os.Getenv("DOCUSIGN_AUTH_SERVER"),
		DocusignOAuthScopes:           envOrDefault("DOCUSIGN_OAUTH_SCOPES", "signature impersonation"),
		DocusignAccountID:             os.Getenv("DOCUSIGN_ACCOUNT_ID"),
		DocusignAPIBaseURI:            os.Getenv("DOCUSIGN_API_BASE_URI"),
		DocusignRequestTimeoutSeconds: envIntOrDefault("DOCUSIGN_REQUEST_TIMEOUT_SECONDS", 30),
		DocusignPageSize:              envIntOrDefault("DOCUSIGN_PAGE_SIZE", 100),

		// Rate limiting
		RateLimitMaxWaitSeconds:       envIntOrDefault("RATE_LIMIT_MAX_WAIT_SECONDS", 300),
		RateLimitFallbackDelaySeconds: envIntOrDefault("RATE_LIMIT_FALLBACK_DELAY_SECONDS", 60),

		// Retry
		RetryMaxAttempts: envIntOrDefault("RETRY_MAX_ATTEMPTS", 5),
		RetryBaseDelayMS: envIntOrDefault("RETRY_BASE_DELAY_MS", 500),
		RetryMaxDelayMS:  envIntOrDefault("RETRY_MAX_DELAY_MS", 30000),

		// MinIO
		MinioEndpoint:   os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey:  os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey:  os.Getenv("MINIO_SECRET_KEY"),
		MinioBucketName: envOrDefault("MINIO_BUCKET_NAME", "docusign-output"),
		MinioUseSSL:     envBoolOrDefault("MINIO_USE_SSL", false),

		// HMAC
		HMACSecretKeyCore:             os.Getenv("HMAC_SECRET_KEY_CORE"),
		HMACTimestampToleranceSeconds: int64(envIntOrDefault("HMAC_TIMESTAMP_TOLERANCE_SECONDS", 300)),

		// Job worker
		MaxConcurrentJobs:             envIntOrDefault("MAX_CONCURRENT_JOBS", 3),
		MaxConcurrentDocusignRequests: envIntOrDefault("MAX_CONCURRENT_DOCUSIGN_REQUESTS", 5),
		HeartbeatIntervalSeconds:      envIntOrDefault("HEARTBEAT_INTERVAL_SECONDS", 30),
		CrashTimeoutMinutes:           envIntOrDefault("CRASH_TIMEOUT_MINUTES", 10),

		// Local output
		LocalOutputDir: envOrDefault("LOCAL_OUTPUT_DIR", "/tmp/docusign-output"),
	}

	return cfg, nil
}

// envOrDefault returns the value of key, or def if the variable is unset or empty.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envIntOrDefault returns the integer value of key, or def on parse failure or absence.
// It does not return an error intentionally — invalid numeric values fall back to the default.
func envIntOrDefault(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// envBoolOrDefault parses "true"/"1"/"yes" as true, everything else as false.
func envBoolOrDefault(key string, def bool) bool {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return def
	}
}

// Sentinel error type for missing configuration.
type MissingConfigError struct {
	Vars []string
}

func (e *MissingConfigError) Error() string {
	return fmt.Sprintf("missing required environment variables: %s", strings.Join(e.Vars, ", "))
}

// IsMissingConfigError reports whether err is a MissingConfigError.
func IsMissingConfigError(err error) bool {
	var t *MissingConfigError
	return errors.As(err, &t)
}
