package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/your-org/docusign-master-service/internal/config"
)

// allRequired is the minimal set of env vars that must be present.
var allRequired = map[string]string{
	"DATABASE_URL":                "postgres://user:pass@localhost:5432/db?sslmode=disable",
	"DOCUSIGN_INTEGRATION_KEY":    "test-integration-key",
	"DOCUSIGN_USER_ID":            "test-user-id",
	"DOCUSIGN_PRIVATE_KEY_PATH":   "/tmp/test.pem",
	"DOCUSIGN_AUTH_SERVER":        "account-d.docusign.com",
	"MINIO_ENDPOINT":              "localhost:9000",
	"MINIO_ACCESS_KEY":            "minioadmin",
	"MINIO_SECRET_KEY":            "minioadmin",
	"HMAC_SECRET_KEY_CORE":        "test-hmac-secret-key",
}

// setEnv sets every key/value pair in the process environment and returns a cleanup func.
func setEnv(t *testing.T, vars map[string]string) func() {
	t.Helper()
	prev := make(map[string]string, len(vars))
	for k, v := range vars {
		prev[k] = os.Getenv(k)
		os.Setenv(k, v)
	}
	return func() {
		for k, p := range prev {
			if p == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, p)
			}
		}
	}
}

// clearAll unsets all required vars so tests start from a clean state.
func clearAll(t *testing.T) func() {
	t.Helper()
	prev := make(map[string]string)
	for k := range allRequired {
		prev[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	return func() {
		for k, p := range prev {
			if p == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, p)
			}
		}
	}
}

func TestLoadConfig_HappyPath(t *testing.T) {
	restore := clearAll(t)
	defer restore()
	cleanup := setEnv(t, allRequired)
	defer cleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if cfg.DatabaseURL != allRequired["DATABASE_URL"] {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, allRequired["DATABASE_URL"])
	}
	if cfg.DocusignIntegrationKey != allRequired["DOCUSIGN_INTEGRATION_KEY"] {
		t.Errorf("DocusignIntegrationKey = %q", cfg.DocusignIntegrationKey)
	}
	if cfg.HMACSecretKeyCore != allRequired["HMAC_SECRET_KEY_CORE"] {
		t.Errorf("HMACSecretKeyCore = %q", cfg.HMACSecretKeyCore)
	}
}

func TestLoadConfig_MissingRequiredVar(t *testing.T) {
	restore := clearAll(t)
	defer restore()

	// Set all vars except DATABASE_URL.
	vars := make(map[string]string)
	for k, v := range allRequired {
		vars[k] = v
	}
	delete(vars, "DATABASE_URL")
	cleanup := setEnv(t, vars)
	defer cleanup()

	_, err := config.LoadConfig()
	if err == nil {
		t.Fatal("expected an error for missing DATABASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should mention DATABASE_URL, got: %v", err)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	restore := clearAll(t)
	defer restore()
	cleanup := setEnv(t, allRequired)
	defer cleanup()

	// Unset optional vars to ensure defaults are applied.
	optionals := []string{
		"PORT", "LOG_LEVEL", "DB_MAX_CONNS", "DB_MIN_CONNS",
		"DOCUSIGN_REQUEST_TIMEOUT_SECONDS", "DOCUSIGN_PAGE_SIZE",
		"RATE_LIMIT_MAX_WAIT_SECONDS", "RATE_LIMIT_FALLBACK_DELAY_SECONDS",
		"RETRY_MAX_ATTEMPTS", "RETRY_BASE_DELAY_MS", "RETRY_MAX_DELAY_MS",
		"HMAC_TIMESTAMP_TOLERANCE_SECONDS",
		"MAX_CONCURRENT_JOBS", "MAX_CONCURRENT_DOCUSIGN_REQUESTS",
		"HEARTBEAT_INTERVAL_SECONDS", "CRASH_TIMEOUT_MINUTES",
		"LOCAL_OUTPUT_DIR",
	}
	for _, k := range optionals {
		os.Unsetenv(k)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Port", cfg.Port, "8080"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"DBMaxConns", cfg.DBMaxConns, int32(10)},
		{"DBMinConns", cfg.DBMinConns, int32(2)},
		{"DocusignRequestTimeoutSeconds", cfg.DocusignRequestTimeoutSeconds, 30},
		{"DocusignPageSize", cfg.DocusignPageSize, 100},
		{"RateLimitMaxWaitSeconds", cfg.RateLimitMaxWaitSeconds, 300},
		{"RateLimitFallbackDelaySeconds", cfg.RateLimitFallbackDelaySeconds, 60},
		{"RetryMaxAttempts", cfg.RetryMaxAttempts, 5},
		{"RetryBaseDelayMS", cfg.RetryBaseDelayMS, 500},
		{"RetryMaxDelayMS", cfg.RetryMaxDelayMS, 30000},
		{"HMACTimestampToleranceSeconds", cfg.HMACTimestampToleranceSeconds, int64(300)},
		{"MaxConcurrentJobs", cfg.MaxConcurrentJobs, 3},
		{"MaxConcurrentDocusignRequests", cfg.MaxConcurrentDocusignRequests, 5},
		{"HeartbeatIntervalSeconds", cfg.HeartbeatIntervalSeconds, 30},
		{"CrashTimeoutMinutes", cfg.CrashTimeoutMinutes, 10},
		{"LocalOutputDir", cfg.LocalOutputDir, "/tmp/docusign-output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
