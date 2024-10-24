package loginbot

import (
	"context"

	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"

	"github.com/Davincible/tgbot"

	tBot "github.com/go-telegram/bot"
)

func (b *Bot) Commands() map[string]func(ctx context.Context, bot *tBot.Bot, update *models.Update) {
	return map[string]func(ctx context.Context, bot *tBot.Bot, update *models.Update){}
}

func (b *Bot) CommandsList() []models.BotCommand {
	return []models.BotCommand{}
}

func (b *Bot) DefaultHandler() tBot.HandlerFunc {
	return func(ctx context.Context, bot *tBot.Bot, update *models.Update) {}
}

func (b *Bot) LoginMiddlware() tBot.Middleware {
	return func(next tBot.HandlerFunc) tBot.HandlerFunc {
		return func(ctx context.Context, bot *tBot.Bot, update *models.Update) {
			if update.Message != nil && b.hasAnyRequests(update.Message.Chat.ID) && hasCode(update.Message.Text) {
				b.handleMessage(ctx, bot, update)
				return
			}

			next(ctx, bot, update)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, bot *tBot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	id := update.Message.Chat.ID

	b.logger.Debug("handling message",
		slog.Int64("id", id),
		slog.String("text", update.Message.Text),
	)

	switch {
	case b.HasOpenReq(id, reqType2Fa):
		b.handle2FACallback(id, update.Message.Text)
	case b.HasOpenReq(id, reqTypeCode):
		b.handleCodeCallback(id, update.Message.Text)
	case b.HasOpenReq(id, reqTypePhone):
		b.handlePhoneCallback(id, update.Message.Text)
	default:
		if _, err := b.sender.Send(id, tgbot.Message{Text: "No open login requests"}); err != nil {
			b.logger.Error("failed to send login reply error", "error", err)
		}
	}
}
