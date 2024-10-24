package tgbot

import (
	"context"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/slog"
)

func TestChainableMerger(t *testing.T) {
	logger := slog.Default()

	// Create the merger
	merger, err := NewBotMerger(MergerConfig{
		ConflictStrategy: SuffixConflicting,
		DefaultSuffix:    "_alt",
		Logger:           logger,
	})
	assert.NoError(t, err)

	// Create some example bots
	bot1 := &ExampleBot{
		commands: map[string]func(ctx context.Context, b *bot.Bot, update *models.Update){
			"/start": func(ctx context.Context, b *bot.Bot, update *models.Update) {},
		},
	}

	bot2 := &ExampleBot{
		commands: map[string]func(ctx context.Context, b *bot.Bot, update *models.Update){
			"/help":  func(ctx context.Context, b *bot.Bot, update *models.Update) {},
			"/start": func(ctx context.Context, b *bot.Bot, update *models.Update) {},
		},
	}

	bot3 := &ExampleBot{
		commands: map[string]func(ctx context.Context, b *bot.Bot, update *models.Update){
			"/settings": func(ctx context.Context, b *bot.Bot, update *models.Update) {},
			"/help":     func(ctx context.Context, b *bot.Bot, update *models.Update) {},
		},
	}

	// Merge all bots
	err = merger.MergeBots(bot1, bot2, bot3)
	assert.NoError(t, err)

	// Verify merged commands
	commands := merger.Commands()
	assert.Contains(t, commands, "/start")
	assert.Contains(t, commands, "/help")
	assert.Contains(t, commands, "/settings")
	assert.Contains(t, commands, "/start_alt") // Conflicting command from bot2
	assert.Contains(t, commands, "/help_alt")  // Conflicting command from bot3
}

// ExampleBot implementation remains the same as before
type ExampleBot struct {
	commands map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)
}

func (eb *ExampleBot) SetSender(b Sender) {}
func (eb *ExampleBot) Commands() map[string]func(ctx context.Context, b *bot.Bot, update *models.Update) {
	return eb.commands
}
func (eb *ExampleBot) CommandsList() []models.BotCommand { return nil }
func (eb *ExampleBot) CallBacks() map[string]CallBack    { return nil }
func (eb *ExampleBot) Middleware() []bot.Middleware      { return nil }
func (eb *ExampleBot) DefaultHandler() bot.HandlerFunc   { return nil }
