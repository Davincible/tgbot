package loginbot

import (
	"fmt"

	"github.com/celestix/gotgproto"
	"golang.org/x/exp/slog"

	"github.com/Davincible/tgbot"
)

var _ gotgproto.AuthConversator = (*Conversator)(nil)

type Conversator struct {
	logger *slog.Logger
	bot    *Bot
	user   int64
	phone  string
}

// NewConversator creates a new conversator sending the requests to the given chatID.
// The phone number is the number to login for.
func (b *Bot) NewConversator(chatID int64, phone string) *Conversator {
	return &Conversator{
		logger: b.logger,
		bot:    b,
		user:   chatID,
		phone:  phone,
	}
}

func (c *Conversator) AskPhoneNumber() (string, error) {
	c.logger.Debug("Asking phone number",
		slog.Int64("user", c.user),
	)

	phone, err := c.bot.AskPhone(c.user)
	if err != nil {
		c.logger.Error("failed to ask phone number",
			slog.String("err", err.Error()),
			slog.Int64("user", c.user),
		)

		return "", fmt.Errorf("failed to ask phone number: %w", err)
	}

	return phone, nil
}

func (c *Conversator) AskCode() (string, error) {
	c.logger.Debug("Asking code",
		slog.Int64("user", c.user),
	)

	code, err := c.bot.SendCodeRequest(c.user)
	if err != nil {
		c.logger.Error("failed to ask code",
			slog.String("err", err.Error()),
			slog.Int64("user", c.user),
		)

		return "", fmt.Errorf("failed to ask code: %w", err)
	}

	return code, nil
}

func (c *Conversator) AskPassword() (string, error) {
	c.logger.Debug("Asking 2FA password",
		slog.Int64("user", c.user),
	)

	code, err := c.bot.Ask2FACode(c.user)
	if err != nil {
		c.logger.Error("failed to ask 2fa code",
			slog.String("err", err.Error()),
			slog.Int64("user", c.user),
		)

		return "", fmt.Errorf("failed to ask code: %w", err)
	}

	return code, nil
}

// SendAuthStatus is called to inform the user about
// the status of the auth process.
// attemptsLeft is the number of attempts left for the user
// to enter the input correctly for the current auth status.
// AuthStatus(authStatus AuthStatus)
func (c *Conversator) AuthStatus(authStatus gotgproto.AuthStatus) {
	var msg *tgbot.Message

	switch authStatus.Event {
	case gotgproto.AuthStatusSuccess:
		msg = &tgbot.Message{
			Text:           fmt.Sprintf(loginSuccessMsg, c.phone),
			TextFormatting: true,
		}
	case gotgproto.AuthStatusFloodWait:
		c.logger.Debug("Telegram Login Auth Timeout",
			slog.String("event", string(authStatus.Event)),
			slog.Time("until", authStatus.Timeout),
		)
		return
	}

	c.logger.Debug("Telegram Login Auth Status",
		slog.String("event", string(authStatus.Event)),
		slog.Int("attempts_left", authStatus.AttemptsLeft),
	)

	if msg == nil {
		return
	}

	if _, err := c.bot.sender.Send(c.user, *msg); err != nil {
		c.logger.Error("failed to send auth status",
			slog.String("err", err.Error()),
		)
	}
}

func (c *Conversator) RetryPassword(attemptsLeft int) (string, error) {
	c.logger.Debug("Retrying 2FA password",
		slog.Int("attempts_left", attemptsLeft),
		slog.Int64("user", c.user),
	)

	code, err := c.bot.Ask2FACode(c.user, attemptsLeft)
	if err != nil {
		c.logger.Error("failed to ask 2fa code",
			slog.String("err", err.Error()),
			slog.Int64("user", c.user),
		)

		return "", fmt.Errorf("failed to ask code: %w", err)
	}

	return code, nil
}
