// service.go
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
	"go.uber.org/ratelimit"
	"golang.org/x/exp/slog"
)

const (
	defaultWorkerPoolSize = 50
	defaultTimeout        = 15 * time.Second
	defaultWebhookTimeout = 30 * time.Second
)

// Sender defines the interface for sending messages and managing telegram content
type Sender interface {
	Send(userID int64, msg Message) (*models.Message, error)
	EditMessage(chatID int64, msgID int, msg Message) (*models.Message, error)
	DeleteMessage(chatID int64, msgID int) error
	DownloadFile(fileID any) ([]byte, error)
	GetProfilePhoto(chatID int64) ([]byte, error)
	BotUsername() string
	SendTyping(chatID int64) error
}

// Bot defines the interface for telegram bot behavior
type Bot interface {
	SetSender(b Sender)
	Commands() map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)
	CommandsList() []models.BotCommand
	CallBacks() map[string]CallBack
	Middleware() []bot.Middleware
	DefaultHandler() bot.HandlerFunc
}

// CallBack represents a telegram callback configuration
type CallBack struct {
	Handler   bot.HandlerFunc
	MatchType bot.MatchType
}

// Config holds the configuration for the telegram service
type Config struct {
	Bot                Bot
	Token              string
	WebhookURL         string
	WebhookSecret      string
	UseWebhook         bool
	Polling            bool
	SkipGetMe          bool
	UseTestEnvironment bool
}

// Service implements the telegram bot service
type Service struct {
	cfg       *Config
	logger    *slog.Logger
	bot       *bot.Bot
	pool      *workerpool.WorkerPool
	username  string
	fileCache *cache.Cache[[]byte]
	ratelimit ratelimit.Limiter
}

// NewService creates a new telegram service instance
func NewService(logger *slog.Logger, cfg *Config) (*Service, error) {
	if err := validateConfig(logger, cfg); err != nil {
		return nil, err
	}

	b, username, err := initializeBot(logger, cfg)
	if err != nil {
		return nil, err
	}

	fileCache, err := cache.New[[]byte](&cache.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create file cache: %w", err)
	}

	srv := &Service{
		cfg:       cfg,
		logger:    logger,
		bot:       b,
		pool:      workerpool.New(defaultWorkerPoolSize),
		username:  username,
		fileCache: fileCache,
		ratelimit: ratelimit.New(30),
	}

	if err := srv.setupBot(); err != nil {
		return nil, err
	}

	return srv, nil
}

func validateConfig(logger *slog.Logger, cfg *Config) error {
	if logger == nil {
		return ErrNilLogger
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if cfg.UseWebhook && len(cfg.WebhookURL) == 0 {
		return fmt.Errorf("webhook setup requested but no webhook URL provided")
	}
	return nil
}

func initializeBot(logger *slog.Logger, cfg *Config) (*bot.Bot, string, error) {
	options := createBotOptions(logger, cfg)
	b, err := bot.New(cfg.Token, options...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create bot: %w", err)
	}

	username := ""
	if !cfg.SkipGetMe {
		self, err := b.GetMe(context.Background())
		if err != nil {
			return nil, "", fmt.Errorf("failed to get bot info: %w", err)
		}
		username = self.Username
	}

	return b, username, nil
}

func (s *Service) setupBot() error {
	if s.cfg.Bot == nil {
		return nil
	}

	s.cfg.Bot.SetSender(s)
	s.registerHandlers()
	s.setupCommands()

	if err := s.setupWebhook(); err != nil {
		s.logger.Error("webhook setup failed",
			slog.String("err", err.Error()),
			slog.String("bot", s.username),
		)
	}

	s.startBot()
	return nil
}

func (s *Service) registerHandlers() {
	for command, handler := range s.cfg.Bot.Commands() {
		s.bot.RegisterHandler(bot.HandlerTypeMessageText, command, bot.MatchTypePrefix, handler)
	}
}

func (s *Service) setupCommands() {
	commandList := s.cfg.Bot.CommandsList()
	if len(commandList) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	_, err := s.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands:     commandList,
		LanguageCode: "en",
	})
	if err != nil {
		s.logger.Error("failed to set bot commands",
			slog.String("err", err.Error()),
			slog.String("bot", s.username),
		)
	}
}

func (s *Service) setupWebhook() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	if _, err := s.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{
		DropPendingUpdates: false,
	}); err != nil {
		return fmt.Errorf("failed to delete webhook: %w", err)
	}

	if !s.cfg.UseWebhook {
		return nil
	}

	s.ensureWebhookSecret()

	_, err := s.bot.SetWebhook(ctx, &bot.SetWebhookParams{
		URL:            s.cfg.WebhookURL,
		SecretToken:    s.cfg.WebhookSecret,
		AllowedUpdates: allowedUpdates,
	})
	return err
}

func (s *Service) ensureWebhookSecret() {
	if len(s.cfg.WebhookSecret) > 0 {
		return
	}

	t := strings.Split(s.cfg.Token, ":")[0]
	secret := fmt.Sprintf("%s-%d", t, time.Now().UnixNano())
	s.cfg.WebhookSecret = hex.EncodeToString(sha256.New().Sum([]byte(secret)))
}

func (s *Service) startBot() {
	if s.cfg.UseWebhook {
		go s.bot.StartWebhook(context.Background())
	} else if s.cfg.Polling {
		go s.bot.Start(context.Background())
	}

	if len(s.username) > 0 {
		s.logger.Debug("Telegram connected", slog.String("bot", s.username))
	} else {
		s.logger.Debug("Telegram connected")
	}
}

// Public methods

func (s *Service) WebhookHandler() http.HandlerFunc {
	return s.bot.WebhookHandler()
}

func (s *Service) Close() {
	s.pool.StopWait()
}

func (s *Service) SendTyping(chatID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultWebhookTimeout)
	defer cancel()

	_, err := s.bot.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionTyping,
	})
	if err != nil {
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	user, err := s.bot.GetMe(ctx)
	if err != nil {
		return ""
	}

	s.username = user.Username
	return user.Username
}
