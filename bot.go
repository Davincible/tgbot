package tgbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

var (
	// allowedUpdates is hardcoded to make sure we receive the reaction updates,
	// which need to be explicitly requested.
	allowedUpdates = []string{
		"message",
		"edited_message",
		"channel_post",
		"edited_channel_post",
		"business_connection",
		"business_message",
		"edited_business_message",
		"deleted_business_messages",
		"message_reaction",
		"message_reaction_count",
		"inline_query",
		"chosen_inline_result",
		"callback_query",
		"shipping_query",
		"pre_checkout_query",
		"poll",
		"poll_answer",
		"my_chat_member",
		"chat_member",
		"chat_join_request",
		"chat_boost",
		"removed_chat_boost",
	}
)

var (
	ErrNilLogger = errors.New("logger not provided")
	ErrNilConfig = errors.New("config not provided")
)

var _ Sender = (*Service)(nil)

type Sender interface {
	Send(userID int64, msg Message) (*models.Message, error)
	EditMessage(chatID int64, msgID int, msg Message) (*models.Message, error)
	DeleteMessage(chatID int64, msgID int) error
	DownloadFile(fileID any) ([]byte, error)
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
	SkippGetMe bool
}

type Service struct {
	cfg    *Config
	logger *slog.Logger
	bot    *bot.Bot
	pool   *workerpool.WorkerPool

	username string
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

	srv := Service{
		cfg:      cfg,
		logger:   logger,
		bot:      b,
		pool:     workerpool.New(50),
		username: username,
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

type Message struct {
	Text               string
	VideoURL           string
	AudioURL           string
	ImageURL           string
	DocumentType       string
	DocumentURL        string
	Document           []byte
	Image              []byte
	Audio              []byte
	Video              []byte
	Entities           []models.MessageEntity
	Buttons            []InlineButton
	ReplyTo            int
	TextFormatting     bool
	DisableLinkPreview bool
}

// hasMedia returns true if the message has any media attachments.
func (m Message) hasMedia() bool {
	return m.VideoURL != "" || m.AudioURL != "" || m.ImageURL != "" ||
		len(m.Document) > 0 || len(m.Image) > 0 || len(m.Audio) > 0 ||
		len(m.Video) > 0 || m.DocumentURL != "" || m.DocumentType != ""
}

// createInputMedia
func (m Message) createInputFile() models.InputMedia {
	if len(m.Image) > 0 || m.ImageURL != "" {
		return &models.InputMediaPhoto{
			Media:           m.ImageURL,
			Caption:         EscapeMarkdown(m.Text, m.TextFormatting),
			ParseMode:       getParseMode(m.TextFormatting),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Video) > 0 || m.VideoURL != "" {
		return &models.InputMediaVideo{
			Media:           m.VideoURL,
			Caption:         EscapeMarkdown(m.Text, m.TextFormatting),
			ParseMode:       getParseMode(m.TextFormatting),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Audio) > 0 || m.AudioURL != "" {
		return &models.InputMediaAudio{
			Media:           m.AudioURL,
			Caption:         EscapeMarkdown(m.Text, m.TextFormatting),
			ParseMode:       getParseMode(m.TextFormatting),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Document) > 0 || m.DocumentURL != "" {
		return &models.InputMediaDocument{
			Media:           m.DocumentURL,
			Caption:         EscapeMarkdown(m.Text, m.TextFormatting),
			ParseMode:       getParseMode(m.TextFormatting),
			CaptionEntities: m.Entities,
		}
	}

	return nil
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
	URL          string `json:"url"`

	Row []InlineButton `json:"row,omitempty"`
}

func (s *Service) Send(chatID int64, msg Message) (*models.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Helper function to handle errors and log them
	handleErr := func(msgType string, err error) error {
		if err != nil {
			s.logger.Error("Error sending message",
				slog.String("err", err.Error()),
				slog.String("type", msgType),
				slog.String("text", EscapeMarkdown(msg.Text, msg.TextFormatting)),
			)

			if strings.Contains(err.Error(), "too long") {
				s.Send(chatID, Message{
					Text: "Message is too long, try a shorter message or without attachment",
				})
			}
		}
		return err
	}

	var replyParams *models.ReplyParameters
	if msg.ReplyTo > 0 {
		replyParams = &models.ReplyParameters{
			ChatID:                   chatID,
			MessageID:                msg.ReplyTo,
			AllowSendingWithoutReply: true,
		}
	}

	var returnMsg *models.Message
	var err error

	switch {
	case len(msg.Image) > 0 || msg.ImageURL != "":
		if returnMsg, err = s.bot.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:          chatID,
			Photo:           createInputFile("image.jpg", msg.Image, msg.ImageURL),
			Caption:         EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:       getParseMode(msg.TextFormatting),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: replyParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("image", err)
		}
	case len(msg.Video) > 0 || msg.VideoURL != "":
		if returnMsg, err = s.bot.SendVideo(ctx, &bot.SendVideoParams{
			ChatID:          chatID,
			Video:           createInputFile("video.mp4", msg.Video, msg.VideoURL),
			Caption:         EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:       getParseMode(msg.TextFormatting),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: replyParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("video", err)
		}
	case len(msg.Audio) > 0 || msg.AudioURL != "":
		if returnMsg, err = s.bot.SendAudio(ctx, &bot.SendAudioParams{
			ChatID:          chatID,
			Audio:           createInputFile("audio.mp3", msg.Audio, msg.AudioURL),
			Caption:         EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:       getParseMode(msg.TextFormatting),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: replyParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("audio", err)
		}
	case msg.DocumentURL != "" || len(msg.Document) > 0:
		if returnMsg, err = s.bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:          chatID,
			Document:        createInputFile("file."+msg.DocumentType, msg.Document, msg.DocumentURL),
			Caption:         EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:       getParseMode(msg.TextFormatting),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: replyParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("document", err)
		}
	case msg.Text != "":
		var previewOpts *models.LinkPreviewOptions
		if msg.DisableLinkPreview {
			t := true
			previewOpts = &models.LinkPreviewOptions{
				IsDisabled: &t,
			}
		}

		if returnMsg, err = s.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:             chatID,
			Text:               EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:          getParseMode(msg.TextFormatting),
			ReplyMarkup:        createInlineKeyboard(msg),
			ReplyParameters:    replyParams,
			Entities:           msg.Entities,
			LinkPreviewOptions: previewOpts,
		}); err != nil {
			return returnMsg, handleErr("text", err)
		}
	default:
		return returnMsg, errors.New("unsupported message type")
	}

	return returnMsg, nil
}

func (s *Service) EditMessage(chatID int64, msgID int, msg Message) (*models.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var previewOpts *models.LinkPreviewOptions
	if msg.DisableLinkPreview {
		t := true
		previewOpts = &models.LinkPreviewOptions{
			IsDisabled: &t,
		}
	}

	var returnMsg *models.Message
	var err error

	if msg.hasMedia() {
		returnMsg, err = s.bot.EditMessageMedia(ctx, &bot.EditMessageMediaParams{
			ChatID:      chatID,
			MessageID:   int(msgID),
			Media:       msg.createInputFile(),
			ReplyMarkup: createInlineKeyboard(msg),
		})
		if err != nil {
			return nil, fmt.Errorf("edit Telegram media: %w", err)
		}
	} else if len(msg.Text) > 0 {
		returnMsg, err = s.bot.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:             chatID,
			MessageID:          int(msgID),
			Text:               EscapeMarkdown(msg.Text, msg.TextFormatting),
			ParseMode:          getParseMode(msg.TextFormatting),
			ReplyMarkup:        createInlineKeyboard(msg),
			Entities:           msg.Entities,
			LinkPreviewOptions: previewOpts,
		})
		if err != nil {
			if strings.Contains(err.Error(), "there is no text in the message to edit") {
				returnMsg, err = s.bot.EditMessageCaption(ctx, &bot.EditMessageCaptionParams{
					ChatID:                chatID,
					MessageID:             int(msgID),
					Caption:               EscapeMarkdown(msg.Text, msg.TextFormatting),
					ParseMode:             getParseMode(msg.TextFormatting),
					CaptionEntities:       msg.Entities,
					DisableWebPagePreview: msg.DisableLinkPreview,
					ReplyMarkup:           createInlineKeyboard(msg),
				})
				if err != nil {
					return nil, fmt.Errorf("edit Telegram caption: %w", err)
				}
			} else {
				return nil, fmt.Errorf("edit Telegram message: %w", err)
			}
		}
	}

	return returnMsg, nil
}

func (s *Service) DeleteMessage(chatID int64, msgID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deleted, err := s.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: msgID,
	})
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	if !deleted {
		return errors.New("unable to delete Telegram message")
	}

	return nil
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

func (s *Service) DownloadFile(fileID any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	file, err := s.bot.GetFile(ctx, &bot.GetFileParams{
		FileID: fmt.Sprintf("%v", fileID),
	})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}

	body, err := downloadFile(fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", s.cfg.Token, file.FilePath))
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}

	return body, nil
}

func (s *Service) GetChat(chatID int64) (*models.ChatFullInfo, error) {
	return s.bot.GetChat(context.Background(), &bot.GetChatParams{
		ChatID: chatID,
	})
}

func (s *Service) GetChatMember(chat, user int64) (*models.ChatMember, error) {
	return s.bot.GetChatMember(context.Background(), &bot.GetChatMemberParams{
		ChatID: chat,
		UserID: user,
	})
}

func (s *Service) GetMe() (*models.User, error) {
	return s.bot.GetMe(context.Background())
}

func (s *Service) GetProfilePhoto(chatID int64) ([]byte, error) {
	fmt.Println("CHECKING PICTURE:", chatID)

	var fileID string
	p, err := s.bot.GetUserProfilePhotos(context.Background(), &bot.GetUserProfilePhotosParams{
		UserID: chatID,
		Limit:  1,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "user not found") {
			return nil, errors.New("user not found")
		}

		// return nil, fmt.Errorf("get user profile photos: %w", err)
		s.logger.Warn("Failed to get user profile photos", slog.String("err", err.Error()))
		chat, err := s.GetChat(chatID)
		if err != nil {
			return nil, fmt.Errorf("get chat: %w", err)
		}

		if chat.Photo == nil {
			return nil, errors.New("no photos found")
		}

		fileID = chat.Photo.BigFileID
	} else {
		if len(p.Photos) == 0 || len(p.Photos[0]) == 0 {
			return nil, errors.New("no photos found")
		}

		getBestPhoto := func(photos [][]models.PhotoSize) *models.PhotoSize {
			var best *models.PhotoSize
			for _, photo := range photos[0] {
				if best == nil || photo.Width > best.Width {
					best = &photo
				}
			}
			return best
		}

		best := getBestPhoto(p.Photos)
		fileID = best.FileID
	}

	if len(fileID) == 0 {
		return nil, errors.New("no picture found")
	}

	return s.DownloadFile(fileID)
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
