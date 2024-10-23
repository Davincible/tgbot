package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"golang.org/x/exp/slog"

	"github.com/Davincible/tgbot/mtproto"
)

func init() {
	if err := godotenv.Load("../../.env"); err != nil {
		panic(fmt.Errorf("error loading .env file: %w", err))
	}
}

func main() {
	logger := setupLogger()
	config := setupConfig()

	logger.Info("Starting Telegram client",
		"appID", config.AppID,
		"phone", config.Phone,
		"dbType", config.DatabaseConfig.Type)

	client, err := mtproto.NewClient(logger, config)
	if err != nil {
		logger.Error("Failed to create client", "error", err)
		os.Exit(1)
	}

	client.WaitUntilLoggedIn()
}

var postgresConfig = mtproto.DatabaseConfig{
	Type:        "postgres",
	DSN:         "postgres://postgres:postgres@localhost:5432/gorm",
	MaxConns:    10,
	TablePrefix: "mtproto_",
}

var sqliteConfig = mtproto.DatabaseConfig{
	Type:     "sqlite",
	DSN:      ":memory:",
	MaxConns: 10,
}

// setupConfig creates and returns a new Config instance
func setupConfig() *mtproto.Config {
	return &mtproto.Config{
		AppID:          getEnvInt("TELEGRAM_APP_ID"),
		APIHash:        getEnv("TELEGRAM_API_HASH"),
		Phone:          getEnv("TELEGRAM_PHONE"),
		DatabaseConfig: postgresConfig,
	}
}

// setupLogger creates and returns a new slog.Logger instance
func setupLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// getEnv retrieves an environment variable with an optional fallback value
func getEnv(name string, fallback ...string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}

	if len(fallback) > 0 {
		return fallback[0]
	}

	return ""
}

// getEnvInt retrieves an environment variable as an integer with an optional fallback value
func getEnvInt(name string, fallback ...int) int {
	if value, ok := os.LookupEnv(name); ok {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}

	if len(fallback) > 0 {
		return fallback[0]
	}

	return 0
}
