package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr            string
	Environment     string
	DatabaseURL     string
	SDKKey          string
	AdminKey        string
	WebRoot         string
	LogLevel        slog.Level
	Heartbeat       time.Duration
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
}

func Load() (Config, error) {
	config := Config{
		Addr:            env("RULECAST_ADDR", ":8090"),
		Environment:     env("RULECAST_ENVIRONMENT", "production"),
		DatabaseURL:     env("RULECAST_DATABASE_URL", ""),
		SDKKey:          env("RULECAST_SDK_KEY", ""),
		AdminKey:        env("RULECAST_ADMIN_KEY", ""),
		WebRoot:         env("RULECAST_WEB_ROOT", "web"),
		Heartbeat:       25 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    0,
	}

	level, err := parseLevel(env("RULECAST_LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	config.LogLevel = level

	if raw := os.Getenv("RULECAST_HEARTBEAT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 {
			return Config{}, fmt.Errorf("RULECAST_HEARTBEAT_SECONDS must be a positive integer, got %q", raw)
		}
		config.Heartbeat = time.Duration(seconds) * time.Second
	}

	if config.Environment == "" {
		return Config{}, fmt.Errorf("RULECAST_ENVIRONMENT must not be empty")
	}

	return config, nil
}

func (c Config) InMemory() bool { return c.DatabaseURL == "" }

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("RULECAST_LOG_LEVEL must be debug, info, warn or error, got %q", raw)
	}
}
