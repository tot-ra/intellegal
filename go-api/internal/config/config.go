package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	logger "github.com/Gratheon/log-lib-go"
)

const defaultInternalToken = "change-me"

// Config holds runtime configuration for the go-api service.
type Config struct {
	Port                 int
	LogLevel             logger.LogLevel
	ShutdownGracePeriod  time.Duration
	CORSAllowedOrigins   []string
	DatabaseURL          string
	DatabasePingTimeout  time.Duration
	InternalServiceToken string
	InternalAIBaseURL    string
	InternalAITimeout    time.Duration
	ExternalCopyBaseURL  string
	ExternalCopyToken    string
	ExternalCopyTimeout  time.Duration
	ExternalCopyRetries  int
	MinIOEndpoint        string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MinIOBucket          string
	MinIOUseSSL          bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:                 8080,
		LogLevel:             logger.LogLevelInfo,
		ShutdownGracePeriod:  10 * time.Second,
		CORSAllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		DatabasePingTimeout:  5 * time.Second,
		InternalServiceToken: defaultInternalToken,
		InternalAIBaseURL:    "http://localhost:8000",
		InternalAITimeout:    5 * time.Minute,
		ExternalCopyTimeout:  8 * time.Second,
		ExternalCopyRetries:  3,
		MinIOEndpoint:        "minio:9000",
		MinIOBucket:          "contracts",
	}

	if v := os.Getenv("GO_API_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			return Config{}, fmt.Errorf("invalid GO_API_PORT: %q", v)
		}
		cfg.Port = port
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level := strings.ToLower(strings.TrimSpace(v))
		switch level {
		case "debug":
			cfg.LogLevel = logger.LogLevelDebug
		case "info":
			cfg.LogLevel = logger.LogLevelInfo
		case "warn", "warning":
			cfg.LogLevel = logger.LogLevelWarn
		case "error":
			cfg.LogLevel = logger.LogLevelError
		default:
			return Config{}, fmt.Errorf("invalid LOG_LEVEL: %q", v)
		}
	}

	if v := os.Getenv("SHUTDOWN_GRACE_PERIOD"); v != "" {
		dur, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHUTDOWN_GRACE_PERIOD: %q", v)
		}
		cfg.ShutdownGracePeriod = dur
	}

	if v := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS")); v != "" {
		parts := strings.Split(v, ",")
		origins := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
		if len(origins) == 0 {
			return Config{}, errors.New("CORS_ALLOWED_ORIGINS is set but empty")
		}
		cfg.CORSAllowedOrigins = origins
	}

	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		cfg.DatabaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_PING_TIMEOUT")); v != "" {
		dur, err := time.ParseDuration(v)
		if err != nil || dur <= 0 {
			return Config{}, fmt.Errorf("invalid DATABASE_PING_TIMEOUT: %q", v)
		}
		cfg.DatabasePingTimeout = dur
	}

	if v := os.Getenv("INTERNAL_SERVICE_TOKEN"); v != "" {
		cfg.InternalServiceToken = v
	}

	if v := strings.TrimSpace(os.Getenv("INTERNAL_AI_BASE_URL")); v != "" {
		cfg.InternalAIBaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("INTERNAL_AI_TIMEOUT")); v != "" {
		dur, err := time.ParseDuration(v)
		if err != nil || dur <= 0 {
			return Config{}, fmt.Errorf("invalid INTERNAL_AI_TIMEOUT: %q", v)
		}
		cfg.InternalAITimeout = dur
	}
	if v := strings.TrimSpace(os.Getenv("EXTERNAL_COPY_API_BASE_URL")); v != "" {
		cfg.ExternalCopyBaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("EXTERNAL_COPY_API_TOKEN")); v != "" {
		cfg.ExternalCopyToken = v
	}
	if v := strings.TrimSpace(os.Getenv("EXTERNAL_COPY_API_TIMEOUT")); v != "" {
		dur, err := time.ParseDuration(v)
		if err != nil || dur <= 0 {
			return Config{}, fmt.Errorf("invalid EXTERNAL_COPY_API_TIMEOUT: %q", v)
		}
		cfg.ExternalCopyTimeout = dur
	}
	if v := strings.TrimSpace(os.Getenv("EXTERNAL_COPY_API_RETRIES")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10 {
			return Config{}, fmt.Errorf("invalid EXTERNAL_COPY_API_RETRIES: %q", v)
		}
		cfg.ExternalCopyRetries = n
	}

	if v := strings.TrimSpace(os.Getenv("MINIO_ENDPOINT")); v != "" {
		cfg.MinIOEndpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIO_ACCESS_KEY")); v != "" {
		cfg.MinIOAccessKey = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIO_SECRET_KEY")); v != "" {
		cfg.MinIOSecretKey = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIO_BUCKET")); v != "" {
		cfg.MinIOBucket = v
	}
	if v := strings.TrimSpace(os.Getenv("MINIO_USE_SSL")); v != "" {
		useSSL, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MINIO_USE_SSL: %q", v)
		}
		cfg.MinIOUseSSL = useSSL
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, errors.New("DATABASE_URL is not set")
	}

	if cfg.InternalServiceToken == defaultInternalToken {
		return Config{}, errors.New("INTERNAL_SERVICE_TOKEN is not set")
	}

	if strings.TrimSpace(cfg.MinIOEndpoint) == "" {
		return Config{}, errors.New("MINIO_ENDPOINT is not set")
	}
	if strings.TrimSpace(cfg.MinIOAccessKey) == "" {
		return Config{}, errors.New("MINIO_ACCESS_KEY is not set")
	}
	if strings.TrimSpace(cfg.MinIOSecretKey) == "" {
		return Config{}, errors.New("MINIO_SECRET_KEY is not set")
	}
	if strings.TrimSpace(cfg.MinIOBucket) == "" {
		return Config{}, errors.New("MINIO_BUCKET is not set")
	}

	return cfg, nil
}
