package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
	URL          string `json:"url"`
	WebAppURL    string `json:"web_app"`

	Row []InlineButton `json:"row,omitempty"`
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
	HTMLFormatting     bool // Enable HTML formatting instead of Markdown
	DisableLinkPreview bool
	ReplyToParams      *models.ReplyParameters
}

func (m *Message) validateButtons() error {
	isCallbackTooBig := func(s string) bool {
		return len([]byte(s)) > 64
	}

	for _, button := range m.Buttons {
		if isCallbackTooBig(button.CallbackData) {
			return fmt.Errorf("callback_data is too big (%d bytes) for button %s: '%s'", len([]byte(button.CallbackData)), button.Text, button.CallbackData)
		}

		for _, rowButton := range button.Row {
			if isCallbackTooBig(rowButton.CallbackData) {
				return fmt.Errorf("callback_data is too big (%d bytes) for button %s: '%s'", len([]byte(rowButton.CallbackData)), rowButton.Text, rowButton.CallbackData)
			}
		}
	}

	return nil
}

func (m *Message) buttons() string {
	var debugStrings []string

	for _, button := range m.Buttons {
		var buttonDebug string

		// Always include Text if available
		if button.Text != "" {
			buttonDebug = fmt.Sprintf("text: %s", strings.ToLower(button.Text))
		}

		// Check for exactly one additional field or mention no additional field
		if button.CallbackData != "" {
			buttonDebug = fmt.Sprintf("%s, callback_data: %s", buttonDebug, strings.ToLower(button.CallbackData))
		} else if button.URL != "" {
			buttonDebug = fmt.Sprintf("%s, url: %s", buttonDebug, strings.ToLower(button.URL))
		} else if button.WebAppURL != "" {
			buttonDebug = fmt.Sprintf("%s, web_app_url: %s", buttonDebug, strings.ToLower(button.WebAppURL))
		} else {
			buttonDebug = fmt.Sprintf("%s, no additional field", buttonDebug)
		}

		// Handle row buttons (nested buttons)
		if len(button.Row) > 0 {
			rowDebugs := []string{}
			for _, rowButton := range button.Row {
				var rowDebug string

				// Always include Text if available
				if rowButton.Text != "" {
					rowDebug = fmt.Sprintf("text: %s", strings.ToLower(rowButton.Text))
				}

				// Check for exactly one additional field or mention no additional field
				if rowButton.CallbackData != "" {
					rowDebug = fmt.Sprintf("%s, callback_data: %s", rowDebug, strings.ToLower(rowButton.CallbackData))
				} else if rowButton.URL != "" {
					rowDebug = fmt.Sprintf("%s, url: %s", rowDebug, strings.ToLower(rowButton.URL))
				} else if rowButton.WebAppURL != "" {
					rowDebug = fmt.Sprintf("%s, web_app_url: %s", rowDebug, strings.ToLower(rowButton.WebAppURL))
				} else {
					rowDebug = fmt.Sprintf("%s, no additional field", rowDebug)
				}

				rowDebugs = append(rowDebugs, fmt.Sprintf("{%s}", rowDebug))
			}

			// Add row debug info to the main button debug
			buttonDebug = fmt.Sprintf("%s, row: [%s]", buttonDebug, strings.Join(rowDebugs, ", "))
		}

		// Add the button's debug string to the overall debug list
		debugStrings = append(debugStrings, fmt.Sprintf("{%s}", buttonDebug))
	}

	// Join all button strings into a single debug output
	return strings.Join(debugStrings, "\n")
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
			Caption:         FormatText(m),
			ParseMode:       getParseMode(m),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Video) > 0 || m.VideoURL != "" {
		return &models.InputMediaVideo{
			Media:           m.VideoURL,
			Caption:         FormatText(m),
			ParseMode:       getParseMode(m),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Audio) > 0 || m.AudioURL != "" {
		return &models.InputMediaAudio{
			Media:           m.AudioURL,
			Caption:         FormatText(m),
			ParseMode:       getParseMode(m),
			CaptionEntities: m.Entities,
		}
	}

	if len(m.Document) > 0 || m.DocumentURL != "" {
		return &models.InputMediaDocument{
			Media:           m.DocumentURL,
			Caption:         FormatText(m),
			ParseMode:       getParseMode(m),
			CaptionEntities: m.Entities,
		}
	}

	return nil
}

func (s *Service) Send(chatID int64, msg Message) (*models.Message, error) {
	s.ratelimit.Take()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Helper function to handle errors and log them
	handleErr := func(msgType string, err error) error {
		if err != nil {
			s.logger.Error("Error sending message",
				slog.String("err", err.Error()),
				slog.String("type", msgType),
				slog.Int64("chat_id", chatID),
				slog.String("text", FormatText(msg)),
				"buttons", msg.buttons(),
			)

			if strings.Contains(err.Error(), "too long") {
				s.Send(chatID, Message{
					Text: "Message is too long, try a shorter message or without attachment",
				})
			}
		}
		return err
	}

	if err := msg.validateButtons(); err != nil {
		return nil, fmt.Errorf("validate buttons: %w", err)
	}

	// if err := s.downloadURLs(msg); err != nil {
	// 	return nil, fmt.Errorf("download URLs: %w", err)
	// }

	if msg.ReplyTo > 0 {
		msg.ReplyToParams = &models.ReplyParameters{
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
			Caption:         FormatText(msg),
			ParseMode:       getParseMode(msg),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: msg.ReplyToParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("image", err)
		}
	case len(msg.Video) > 0 || msg.VideoURL != "":
		if returnMsg, err = s.bot.SendVideo(ctx, &bot.SendVideoParams{
			ChatID:          chatID,
			Video:           createInputFile("video.mp4", msg.Video, msg.VideoURL),
			Caption:         FormatText(msg),
			ParseMode:       getParseMode(msg),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: msg.ReplyToParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("video", err)
		}
	case len(msg.Audio) > 0 || msg.AudioURL != "":
		if returnMsg, err = s.bot.SendAudio(ctx, &bot.SendAudioParams{
			ChatID:          chatID,
			Audio:           createInputFile("audio.mp3", msg.Audio, msg.AudioURL),
			Caption:         FormatText(msg),
			ParseMode:       getParseMode(msg),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: msg.ReplyToParams,
			CaptionEntities: msg.Entities,
		}); err != nil {
			return returnMsg, handleErr("audio", err)
		}
	case msg.DocumentURL != "" || len(msg.Document) > 0:
		if returnMsg, err = s.bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:          chatID,
			Document:        createInputFile("file."+msg.DocumentType, msg.Document, msg.DocumentURL),
			Caption:         FormatText(msg),
			ParseMode:       getParseMode(msg),
			ReplyMarkup:     createInlineKeyboard(msg),
			ReplyParameters: msg.ReplyToParams,
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
			Text:               FormatText(msg),
			ParseMode:          getParseMode(msg),
			ReplyMarkup:        createInlineKeyboard(msg),
			ReplyParameters:    msg.ReplyToParams,
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
	s.ratelimit.Take()

	// Helper function to handle errors and log them
	handleErr := func(msgType string, err error) error {
		if err != nil {
			s.logger.Error("Error sending message",
				slog.String("err", err.Error()),
				slog.String("type", msgType),
				slog.Int64("chat_id", chatID),
				slog.String("text", FormatText(msg)),
				"buttons", msg.buttons(),
			)

			if strings.Contains(err.Error(), "too long") {
				s.Send(chatID, Message{
					Text: "Message is too long, try a shorter message or without attachment",
				})
			}
		}
		return err
	}

	if err := msg.validateButtons(); err != nil {
		return nil, fmt.Errorf("validate buttons: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var previewOpts *models.LinkPreviewOptions
	if msg.DisableLinkPreview {
		t := true
		previewOpts = &models.LinkPreviewOptions{
			IsDisabled: &t,
		}
	}

	// if err := s.downloadURLs(msg); err != nil {
	// 	return nil, fmt.Errorf("download URLs: %w", err)
	// }

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
			return nil, handleErr("media", err)
		}
	} else if len(msg.Text) > 0 {
		returnMsg, err = s.bot.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:             chatID,
			MessageID:          int(msgID),
			Text:               FormatText(msg),
			ParseMode:          getParseMode(msg),
			ReplyMarkup:        createInlineKeyboard(msg),
			Entities:           msg.Entities,
			LinkPreviewOptions: previewOpts,
		})
		if err != nil {
			if strings.Contains(err.Error(), "there is no text in the message to edit") {
				returnMsg, err = s.bot.EditMessageCaption(ctx, &bot.EditMessageCaptionParams{
					ChatID:                chatID,
					MessageID:             int(msgID),
					Caption:               FormatText(msg),
					ParseMode:             getParseMode(msg),
					CaptionEntities:       msg.Entities,
					DisableWebPagePreview: msg.DisableLinkPreview,
					ReplyMarkup:           createInlineKeyboard(msg),
				})
				if err != nil {
					return nil, handleErr("caption", err)
				}
			} else {
				return nil, handleErr("text", err)
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
