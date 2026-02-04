package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	APIID            int32
	APIHash          string
	Phone            string
	HTTPAddr         string
	HTTPPort         string
	TDLIBDatabaseDir string
	TDLIBLogLevel    int32
	LogLevel         slog.Level
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	apiIDStr := getEnv("API_ID", "")
	if apiIDStr == "" {
		return nil, fmt.Errorf("API_ID is required")
	}

	apiID64, err := strconv.ParseInt(apiIDStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid API_ID: %w", err)
	}

	tdlibLogLevelStr := getEnv("TDLIB_LOG_LEVEL", "0")
	tdlibLogLevel, err := strconv.ParseInt(tdlibLogLevelStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid TDLIB_LOG_LEVEL: %w", err)
	}

	logLevel, err := parseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		return nil, fmt.Errorf("Invalid LOG_LEVEL: %w", err)
	}

	cfg := &Config{
		APIID:            int32(apiID64),
		APIHash:          getEnv("API_HASH", ""),
		Phone:            getEnv("PHONE", ""),
		HTTPAddr:         getEnv("HTTP_ADDR", "127.0.0.1"),
		HTTPPort:         getEnv("HTTP_PORT", "8080"),
		TDLIBDatabaseDir: getEnv("TDLIB_DATABASE_DIR", "./data"),
		TDLIBLogLevel:    int32(tdlibLogLevel),
		LogLevel:         logLevel,
	}

	// Validation
	if cfg.APIHash == "" {
		return nil, fmt.Errorf("TELEGRAM_API_HASH is required")
	}
	if cfg.Phone == "" {
		return nil, fmt.Errorf("TELEGRAM_PHONE is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level: %q (expected: debug, info, warn, warning, error)", s)
	}
}
