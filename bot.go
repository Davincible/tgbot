package tgbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Davincible/cache"
	"github.com/gammazero/workerpool"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

var _ Sender = (*Service)(nil)

type Sender interface {
	Send(userID int64, msg Message) (*models.Message, error)
	EditMessage(chatID int64, msgID int, msg Message) (*models.Message, error)
	DeleteMessage(chatID int64, msgID int) error
	DownloadFile(fileID any) ([]byte, error)
	GetProfilePhoto(chatID int64) ([]byte, error)
	BotUsername() string
}

type CallBack struct {
	Handler   bot.HandlerFunc
	MatchType bot.MatchType
}

type Bot interface {
	SetSender(b Sender)
	Commands() map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)
	CommandsList() []models.BotCommand
	CallBacks() map[string]CallBack
	Middleware() []bot.Middleware
	DefaultHandler() bot.HandlerFunc
}

type Config struct {
	Bot           Bot
	Token         string
	WebhookURL    string
	WebhookSecret string
	UseWebhook    bool
	Polling       bool
	// SkipGetMe skips the GetMe call on bot creation.
	SkippGetMe         bool
	UseTestEnvironment bool
}

type Service struct {
	cfg    *Config
	logger *slog.Logger
	bot    *bot.Bot
	pool   *workerpool.WorkerPool

	username  string
	fileCache *cache.Cache[[]byte]
}

func NewService(logger *slog.Logger, cfg *Config) (*Service, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if cfg == nil {
		return nil, ErrNilConfig
	}

	logger.Debug("Creating Telegram bot")

	var username string

	options := []bot.Option{
		bot.WithAllowedUpdates(allowedUpdates),
		bot.WithCheckInitTimeout(15 * time.Second),
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
		}),
		bot.WithDebugHandler(func(format string, args ...any) {
			logger.Debug(fmt.Sprintf(format, args...))
		}),
		bot.WithErrorsHandler(func(err error) {
			logger.Error(err.Error(),
				slog.String("bot", username),
			)
		}),
	}

	if cfg.UseTestEnvironment {
		options = append(options, bot.UseTestEnvironment())
	}

	if cfg.Bot != nil {
		for pattern, callback := range cfg.Bot.CallBacks() {
			options = append(options, bot.WithCallbackQueryDataHandler(pattern, callback.MatchType, callback.Handler))
		}

		if middleware := cfg.Bot.Middleware(); len(middleware) > 0 {
			captionCmdMiddleware := func(next bot.HandlerFunc) bot.HandlerFunc {
				return func(ctx context.Context, b *bot.Bot, update *models.Update) {
					// If commands are called from a caption, e.g. image, video, or document, they are not registered by default
					if update.Message != nil && update.Message.Caption != "" && cfg.Bot != nil {
						for command, handler := range cfg.Bot.Commands() {
							if strings.HasPrefix(update.Message.Text, command) {
								handler(ctx, b, update)
								return
							}

							if update.Message.Caption != "" && strings.HasPrefix(update.Message.Caption, command) {
								handler(ctx, b, update)
								return
							}
						}
					}

					next(ctx, b, update)
				}
			}

			options = append(options, bot.WithMiddlewares(append(middleware, captionCmdMiddleware)...))
		}

		if defaultHandler := cfg.Bot.DefaultHandler(); defaultHandler != nil {
			options = append(options, bot.WithDefaultHandler(defaultHandler))
		}
	}

	b, err := bot.New(cfg.Token, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	if !cfg.SkippGetMe {
		self, err := b.GetMe(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to get bot info: %w", err)
		}

		username = self.Username
	}

	fileCache, err := cache.New[[]byte](&cache.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create file cache: %w", err)
	}

	srv := Service{
		cfg:       cfg,
		logger:    logger,
		bot:       b,
		pool:      workerpool.New(50),
		username:  username,
		fileCache: fileCache,
	}

	if cfg.Bot != nil {
		cfg.Bot.SetSender(&srv)

		for command, handler := range cfg.Bot.Commands() {
			b.RegisterHandler(bot.HandlerTypeMessageText, command, bot.MatchTypePrefix, handler)
		}

		if commandList := cfg.Bot.CommandsList(); len(commandList) > 0 {
			if _, err = b.SetMyCommands(context.Background(), &bot.SetMyCommandsParams{
				Commands:     commandList,
				LanguageCode: "en",
			}); err != nil {
				logger.Error("failed to set bot commands",
					slog.String("err", err.Error()),

					slog.String("bot", username),
				)
			}
		}
	}

	if _, err = b.DeleteWebhook(context.Background(), &bot.DeleteWebhookParams{
		DropPendingUpdates: false,
	}); err != nil {
		logger.Error("failed to delete webhook",
			slog.String("err", err.Error()),
			slog.String("bot", username),
		)
	}

	if cfg.UseWebhook {
		logger.Debug("Setting up Telegram webhook", slog.String("url", cfg.WebhookURL))

		if len(cfg.WebhookURL) == 0 {
			return nil, fmt.Errorf("webhook setup requested but no webhook url provided")
		}

		// Randomly generate a secret token if none is provided
		if len(cfg.WebhookSecret) == 0 {
			t := strings.Split(cfg.Token, ":")[0]
			secret := fmt.Sprintf("%s-%d", t, time.Now().UnixNano())
			cfg.WebhookSecret = hex.EncodeToString(sha256.New().Sum([]byte(secret)))
		}

		if _, err = b.SetWebhook(context.Background(), &bot.SetWebhookParams{
			URL:            cfg.WebhookURL,
			SecretToken:    cfg.WebhookSecret,
			AllowedUpdates: allowedUpdates,
		}); err != nil {
			logger.Error("failed to set webhook",
				slog.String("err", err.Error()),
				slog.String("secret", cfg.WebhookSecret),
				slog.String("url", cfg.WebhookURL),
				slog.String("bot", username),
			)
		}

		go b.StartWebhook(context.Background())
	} else if cfg.Polling {
		go b.Start(context.Background())
	}

	if len(username) > 0 {
		logger.Debug("Telegram connected", slog.String("bot", username))
	} else {
		logger.Debug("Telegram connected")
	}

	return &srv, nil
}

func (s *Service) WebhookHandler() http.HandlerFunc {
	return s.bot.WebhookHandler()
}

func (s *Service) Close() {
	s.pool.StopWait()
}

func (s *Service) SendTyping(chatID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := s.bot.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionTyping,
	}); err != nil {
		return fmt.Errorf("failed to send typing action: %w", err)
	}

	return nil
}

func (s *Service) GetMe() (*models.User, error) {
	return s.bot.GetMe(context.Background())
}

func (s *Service) BotUsername() string {
	if len(s.username) > 0 {
		return s.username
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := s.bot.GetMe(ctx)
	if err != nil {
		return ""
	}

	s.username = user.Username

	return user.Username
}
