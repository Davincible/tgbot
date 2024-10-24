package tgbot

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

// BotMerger implements both a merger utility and the Bot interface
type BotMerger struct {
	sync.RWMutex
	commands   map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)
	callbacks  map[string]CallBack
	middleware []bot.Middleware
	sender     Sender
	logger     *slog.Logger
	config     MergerConfig

	defaultHandlers []bot.HandlerFunc
}

// MergerConfig defines the configuration for the bot merger
type MergerConfig struct {
	// ConflictStrategy determines how to handle command conflicts
	ConflictStrategy ConflictStrategy
	// DefaultSuffix is used when SuffixConflicting strategy is chosen
	DefaultSuffix string
	// FailOnConflict if true will return error when conflicts are found (overrides ConflictStrategy)
	FailOnConflict bool
	// Logger for merger operations
	Logger *slog.Logger
}

// ConflictStrategy determines how to handle conflicts during merge
type ConflictStrategy int

const (
	// KeepOriginal keeps the first encountered version of conflicting items
	KeepOriginal ConflictStrategy = iota
	// ReplaceWithNew replaces with the new version when conflicts occur
	ReplaceWithNew
	// SuffixConflicting adds a suffix to conflicting items
	SuffixConflicting
)

// NewBotMerger creates a new bot merger instance
func NewBotMerger(config MergerConfig) (*BotMerger, error) {
	if err := config.validateConfig(); err != nil {
		return nil, fmt.Errorf("invalid merger config: %w", err)
	}

	return &BotMerger{
		commands:   make(map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)),
		callbacks:  make(map[string]CallBack),
		middleware: make([]bot.Middleware, 0),
		logger:     config.Logger,
		config:     config,
	}, nil
}

// MergeBots merges multiple bots into the merger
func (m *BotMerger) MergeBots(bots ...Bot) error {
	m.Lock()
	defer m.Unlock()

	for _, bot := range bots {
		if err := m.mergeBot(bot); err != nil {
			return fmt.Errorf("failed to merge bot: %w", err)
		}
	}

	return nil
}

func (m *BotMerger) mergeBot(bot Bot) error {
	if err := m.mergeCommands(bot.Commands()); err != nil {
		return err
	}

	if err := m.mergeCallbacks(bot.CallBacks()); err != nil {
		return err
	}

	m.middleware = append(m.middleware, bot.Middleware()...)
	m.defaultHandlers = append(m.defaultHandlers, bot.DefaultHandler())

	// Set the sender on the merged bot
	if m.sender != nil {
		bot.SetSender(m.sender)
	}

	return nil
}

func (m *BotMerger) mergeCommands(newCmds map[string]func(ctx context.Context, b *bot.Bot, update *models.Update)) error {
	for cmd, handler := range newCmds {
		if existing, exists := m.commands[cmd]; exists {
			if err := m.handleCommandConflict(cmd, handler, existing); err != nil {
				return err
			}
			continue
		}
		m.commands[cmd] = handler
	}
	return nil
}

func (m *BotMerger) handleCommandConflict(cmd string, newHandler, existingHandler func(ctx context.Context, b *bot.Bot, update *models.Update)) error {
	if m.config.FailOnConflict {
		return fmt.Errorf("command conflict detected: %s", cmd)
	}

	switch m.config.ConflictStrategy {
	case KeepOriginal:
		m.logger.Info("keeping original command",
			slog.String("command", cmd))
	case ReplaceWithNew:
		m.commands[cmd] = newHandler
		m.logger.Info("replaced command with new version",
			slog.String("command", cmd))
	case SuffixConflicting:
		newCmd := cmd + m.config.DefaultSuffix
		m.commands[newCmd] = newHandler
		m.logger.Info("added suffixed command",
			slog.String("original", cmd),
			slog.String("suffixed", newCmd))
	}

	return nil
}

func (m *BotMerger) mergeCallbacks(newCallbacks map[string]CallBack) error {
	for pattern, callback := range newCallbacks {
		if existing, exists := m.callbacks[pattern]; exists {
			if err := m.handleCallbackConflict(pattern, callback, existing); err != nil {
				return err
			}
			continue
		}
		m.callbacks[pattern] = callback
	}
	return nil
}

func (m *BotMerger) handleCallbackConflict(pattern string, newCallback, existingCallback CallBack) error {
	if m.config.FailOnConflict {
		return fmt.Errorf("callback conflict detected: %s", pattern)
	}

	switch m.config.ConflictStrategy {
	case KeepOriginal:
		m.logger.Info("keeping original callback",
			slog.String("pattern", pattern))
	case ReplaceWithNew:
		m.callbacks[pattern] = newCallback
		m.logger.Info("replaced callback with new version",
			slog.String("pattern", pattern))
	case SuffixConflicting:
		newPattern := m.config.DefaultSuffix + pattern
		m.callbacks[newPattern] = newCallback
		m.logger.Info("added suffixed callback",
			slog.String("original", pattern),
			slog.String("suffixed", newPattern))
	}

	return nil
}

// Bot interface implementation

func (m *BotMerger) SetSender(s Sender) {
	m.Lock()
	defer m.Unlock()
	m.sender = s
}

func (m *BotMerger) Commands() map[string]func(ctx context.Context, b *bot.Bot, update *models.Update) {
	m.RLock()
	defer m.RUnlock()
	return m.commands
}

func (m *BotMerger) CommandsList() []models.BotCommand {
	m.RLock()
	defer m.RUnlock()

	var list []models.BotCommand
	for cmd := range m.commands {
		list = append(list, models.BotCommand{
			Command:     strings.TrimSuffix(cmd, "/"),
			Description: fmt.Sprintf("Merged command: %s", cmd),
		})
	}
	return list
}

func (m *BotMerger) CallBacks() map[string]CallBack {
	m.RLock()
	defer m.RUnlock()

	return m.callbacks
}

func (m *BotMerger) Middleware() []bot.Middleware {
	m.RLock()
	defer m.RUnlock()

	return m.middleware
}

func (m *BotMerger) DefaultHandler() bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		for _, handler := range m.defaultHandlers {
			handler(ctx, b, update)
		}
	}
}

func (config *MergerConfig) validateConfig() error {
	if config.Logger == nil {
		return fmt.Errorf("logger cannot be nil")
	}

	if config.ConflictStrategy == SuffixConflicting && config.DefaultSuffix == "" {
		return fmt.Errorf("default suffix must be set when using SuffixConflicting strategy")
	}

	return nil
}