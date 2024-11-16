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

	// if err := s.downloadURLs(msg); err != nil {
	// 	return nil, fmt.Errorf("download URLs: %w", err)
	// }

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
	s.ratelimit.Take()

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
