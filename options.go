package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

// createBotOptions creates the configuration options for the telegram bot
func createBotOptions(logger *slog.Logger, cfg *Config) []bot.Option {
	options := []bot.Option{
		bot.WithAllowedUpdates(allowedUpdates),
		bot.WithCheckInitTimeout(defaultTimeout),
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {}),
		createDebugHandler(logger),
		createErrorHandler(logger),
	}

	if cfg.UseTestEnvironment {
		options = append(options, bot.UseTestEnvironment())
	}

	if cfg.Bot != nil {
		options = append(options, createBotSpecificOptions(cfg.Bot)...)
	}

	return options
}

func createDebugHandler(logger *slog.Logger) bot.Option {
	return bot.WithDebugHandler(func(format string, args ...any) {
		logger.Debug(fmt.Sprintf(format, args...))
	})
}

func createErrorHandler(logger *slog.Logger) bot.Option {
	return bot.WithErrorsHandler(func(err error) {
		logger.Error(err.Error())
	})
}

func createBotSpecificOptions(b Bot) []bot.Option {
	var options []bot.Option

	// Add callback handlers
	for pattern, callback := range b.CallBacks() {
		options = append(options, bot.WithCallbackQueryDataHandler(
			pattern,
			callback.MatchType,
			callback.Handler,
		))
	}

	// Add middleware
	if middleware := b.Middleware(); len(middleware) > 0 {
		options = append(options, bot.WithMiddlewares(
			append(middleware, createCaptionCommandMiddleware(b))...,
		))
	}

	// Add default handler
	if defaultHandler := b.DefaultHandler(); defaultHandler != nil {
		options = append(options, bot.WithDefaultHandler(defaultHandler))
	}

	return options
}

func createCaptionCommandMiddleware(bb Bot) bot.Middleware {
	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, b *bot.Bot, update *models.Update) {
			if update.Message == nil || update.Message.Caption == "" {
				next(ctx, b, update)
				return
			}

			for command, handler := range bb.Commands() {
				if strings.HasPrefix(update.Message.Text, command) ||
					strings.HasPrefix(update.Message.Caption, command) {
					handler(ctx, b, update)
					return
				}
			}

			next(ctx, b, update)
		}
	}
}
