package mtproto

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/joho/godotenv"
	"github.com/test-go/testify/require"
	"golang.org/x/exp/slog"

	"github.com/Davincible/tgbot"
	"github.com/Davincible/tgbot/bots/loginbot"
)

var (
	chats = map[string]int64{
		"david": 739125269,
	}
)

func init() {
	if err := godotenv.Load("../.env"); err != nil {
		panic(fmt.Errorf("Error loading .env file: %w", err))
	}
}

// Test setup helpers
func setupTestConfig() *Config {
	return &Config{
		AppID:   getEnvInt("TELEGRAM_APP_ID"),
		APIHash: getEnv("TELEGRAM_API_HASH"),
		Phone:   getEnv("TELEGRAM_PHONE"),
		DatabaseConfig: DatabaseConfig{
			Type:     "sqlite",
			DSN:      ":memory:",
			MaxConns: 10,
		},
	}
}

func setupTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func TestLogin(t *testing.T) {
	logger := setupTestLogger()

	loginBot := loginbot.New(logger, loginbot.Config{})
	tgSrv, err := tgbot.NewService(logger, &tgbot.Config{
		Bot:     loginBot,
		Token:   getEnv("TELEGRAM_BOT_TOKEN"),
		Polling: true,
	})
	require.NoError(t, err, "Setup telegram service")
	defer tgSrv.Close()

	t.Log("TestLogin: Setup NewClient")

	client, err := NewClient(logger, &Config{
		AppID:           getEnvInt("TELEGRAM_APP_ID"),
		APIHash:         getEnv("TELEGRAM_API_HASH"),
		Phone:           getEnv("TELEGRAM_PHONE"),
		AuthConversator: loginBot.NewConversator(chats["david"], getEnv("TELEGRAM_PHONE")),
		DatabaseConfig: DatabaseConfig{
			Type:     "sqlite",
			DSN:      ":memory:",
			MaxConns: 10,
		},
	})
	require.NoError(t, err, "Setup NewClient")

	client.WaitUntilLoggedIn()
}

func getEnv(name string, fallback ...string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}

	if len(fallback) > 0 {
		return fallback[0]
	}

	return ""
}

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
